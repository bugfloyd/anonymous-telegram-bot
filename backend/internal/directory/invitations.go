package directory

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

const (
	codePrefix  = "I#"
	skCode      = "I"
	levelsPK    = "S#levels"
	skSettings  = "S"
	maxListed   = 50
	levelsTries = 3
)

var (
	// ErrAlreadyMember is returned when redeeming for an account that is a member.
	ErrAlreadyMember = errors.New("directory: already a member")
	// ErrOwnInvitation is returned when someone redeems a code they made.
	ErrOwnInvitation = errors.New("directory: own invitation")
	// ErrNotMember is returned when granting invitations to an account that isn't a member.
	ErrNotMember = errors.New("directory: not a member")
)

// Invitation is a code and how much of it is used.
type Invitation struct {
	Code string // canonical, "abcd-efgh"
	Uses int
	Left int
}

// codeKey stores a code without its dash.
func codeKey(code string) string { return strings.ReplaceAll(code, "-", "") }

func codePK(key string) string { return "I#" + key }

func codeFromKey(key string) string {
	if len(key) != 8 {
		return key
	}
	return key[:4] + "-" + key[4:]
}

// Levels returns the invitation allowance of every level that has one.
func (s *Store) Levels(ctx context.Context) (map[int]int, error) {
	levels, _, err := s.levels(ctx)
	return levels, err
}

func (s *Store) levels(ctx context.Context) (map[int]int, int64, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{TableName: &s.Table, Key: key(levelsPK, skSettings), ConsistentRead: aws.Bool(true)})
	if err != nil {
		return nil, 0, err
	}
	levels := map[int]int{}
	if m, ok := out.Item["m"].(*types.AttributeValueMemberM); ok {
		for k, v := range m.Value {
			level, err1 := strconv.Atoi(k)
			n, ok := v.(*types.AttributeValueMemberN)
			if err1 != nil || !ok {
				continue
			}
			allowance, err2 := strconv.Atoi(n.Value)
			if err2 == nil {
				levels[level] = allowance
			}
		}
	}
	return levels, num(out.Item, "v"), nil
}

// SetLevel sets a level's allowance, for existing and new members alike.
func (s *Store) SetLevel(ctx context.Context, level, allowance int) error {
	for try := 0; ; try++ {
		levels, version, err := s.levels(ctx)
		if err != nil {
			return err
		}
		levels[level] = allowance
		m := attrs{}
		for l, n := range levels {
			m[strconv.Itoa(l)] = nv(int64(n))
		}
		cond := "attribute_not_exists(pk)"
		values := attrs{}
		if version > 0 {
			cond = "v = :v"
			values[":v"] = nv(version)
		}
		input := &dynamodb.PutItemInput{
			TableName:           &s.Table,
			Item:                item(levelsPK, skSettings, attrs{"m": &types.AttributeValueMemberM{Value: m}, "v": nv(version + 1)}),
			ConditionExpression: aws.String(cond),
		}
		if len(values) > 0 {
			input.ExpressionAttributeValues = values
		}
		_, err = s.DB.PutItem(ctx, input)
		var failed *types.ConditionalCheckFailedException
		if errors.As(err, &failed) && try < levelsTries {
			continue // a concurrent change: read again
		}
		return err
	}
}

// CreateInvitation stores a code. ownerLevel is 0 for admins. For members,
// maxAllocated is the most uses they may have put into codes after this one
// (their allowance plus grants); admins pass -1 for no limit. It returns
// ErrConflict if the code is taken, and ErrLimit if the member has too few
// invitations left.
func (s *Store) CreateInvitation(ctx context.Context, accountID, code string, uses, ownerLevel, maxAllocated int) error {
	k := codeKey(code)
	writes := []types.TransactWriteItem{
		{Put: &types.Put{
			TableName: &s.Table,
			Item: item(codePK(k), skCode, attrs{
				"o": sv(accountID),
				"l": nv(int64(ownerLevel)),
				"u": nv(int64(uses)),
				"r": nv(int64(uses)),
			}),
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		}},
		{Put: &types.Put{TableName: &s.Table, Item: item(accountPK(accountID), codePrefix+k, attrs{})}},
	}
	if maxAllocated >= 0 {
		writes = append(writes, types.TransactWriteItem{Update: &types.Update{
			TableName:           &s.Table,
			Key:                 key(accountPK(accountID), skAccount),
			UpdateExpression:    aws.String("SET ia = if_not_exists(ia, :zero) + :uses"),
			ConditionExpression: aws.String("attribute_exists(lv) AND (attribute_not_exists(ia) OR ia <= :most)"),
			ExpressionAttributeValues: attrs{
				":zero": nv(0),
				":uses": nv(int64(uses)),
				":most": nv(int64(maxAllocated - uses)),
			},
		}})
	}
	reasons, err := s.transactReasons(ctx, writes)
	if errors.Is(err, ErrConflict) && len(reasons) == 3 && reasons[2] == "ConditionalCheckFailed" {
		return ErrLimit
	}
	return err
}

// Invitations lists an account's codes (at most 50), in code order.
func (s *Store) Invitations(ctx context.Context, accountID string) ([]Invitation, error) {
	out, err := s.DB.Query(ctx, &dynamodb.QueryInput{
		TableName:              &s.Table,
		KeyConditionExpression: aws.String("pk = :pk AND begins_with(sk, :prefix)"),
		ExpressionAttributeValues: attrs{
			":pk":     sv(accountPK(accountID)),
			":prefix": sv(codePrefix),
		},
		Limit: aws.Int32(maxListed),
	})
	if err != nil || len(out.Items) == 0 {
		return nil, err
	}
	keys := make([]map[string]types.AttributeValue, 0, len(out.Items))
	for _, it := range out.Items {
		sk, _ := str(it, "sk")
		keys = append(keys, key(codePK(strings.TrimPrefix(sk, codePrefix)), skCode))
	}
	var items []map[string]types.AttributeValue
	request := map[string]types.KeysAndAttributes{s.Table: {Keys: keys, ConsistentRead: aws.Bool(true)}}
	for attempt := 0; len(request) > 0; attempt++ {
		if attempt == maxBatchRetries {
			return nil, errors.New("directory: batch read left keys unprocessed")
		}
		if attempt > 0 {
			time.Sleep(time.Duration(50<<attempt) * time.Millisecond)
		}
		got, err := s.DB.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{RequestItems: request})
		if err != nil {
			return nil, err
		}
		items = append(items, got.Responses[s.Table]...)
		request = got.UnprocessedKeys
	}
	list := make([]Invitation, 0, len(items))
	for _, it := range items {
		pk, _ := str(it, "pk")
		list = append(list, Invitation{
			Code: codeFromKey(strings.TrimPrefix(pk, "I#")),
			Uses: int(num(it, "u")),
			Left: int(num(it, "r")),
		})
	}
	slices.SortFunc(list, func(a, b Invitation) int { return strings.Compare(a.Code, b.Code) })
	return list, nil
}

// Redeem uses one use of a code to make the account a member, one level below
// the code's owner, and returns that level. It returns ErrNotFound for an
// unknown or used-up code, and ErrAlreadyMember if the account is a member.
func (s *Store) Redeem(ctx context.Context, accountID, code string) (int, error) {
	k := codeKey(code)
	it, err := s.get(ctx, codePK(k), skCode, time.Time{})
	if err != nil {
		return 0, err
	}
	if owner, _ := str(it, "o"); owner == accountID {
		return 0, ErrOwnInvitation
	}
	level := int(num(it, "l")) + 1
	writes := []types.TransactWriteItem{
		{Update: &types.Update{
			TableName:                 &s.Table,
			Key:                       key(codePK(k), skCode),
			UpdateExpression:          aws.String("SET r = r - :one"),
			ConditionExpression:       aws.String("r > :zero"),
			ExpressionAttributeValues: attrs{":one": nv(1), ":zero": nv(0)},
		}},
		{Update: &types.Update{
			TableName:                 &s.Table,
			Key:                       key(accountPK(accountID), skAccount),
			UpdateExpression:          aws.String("SET lv = :level"),
			ConditionExpression:       aws.String("attribute_exists(ak) AND attribute_not_exists(lv)"),
			ExpressionAttributeValues: attrs{":level": nv(int64(level))},
		}},
	}
	reasons, err := s.transactReasons(ctx, writes)
	if errors.Is(err, ErrConflict) && len(reasons) == 2 {
		switch {
		case reasons[0] == "ConditionalCheckFailed":
			return 0, ErrNotFound
		case reasons[1] == "ConditionalCheckFailed":
			return 0, ErrAlreadyMember
		}
	}
	return level, err
}

// Grant gives the member who owns a link extra invitations. It returns
// ErrNotFound for an unknown link and ErrNotMember if its owner isn't a member.
func (s *Store) Grant(ctx context.Context, linkID string, invitations int) error {
	it, err := s.get(ctx, linkPK(linkID), skLink, time.Time{})
	if err != nil {
		return err
	}
	owner, ok := str(it, "a")
	if !ok {
		return ErrNotFound
	}
	_, err = s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Table,
		Key:                       key(accountPK(owner), skAccount),
		UpdateExpression:          aws.String("SET ig = if_not_exists(ig, :zero) + :n"),
		ConditionExpression:       aws.String("attribute_exists(lv)"),
		ExpressionAttributeValues: attrs{":zero": nv(0), ":n": nv(int64(invitations))},
	})
	var failed *types.ConditionalCheckFailedException
	if errors.As(err, &failed) {
		return ErrNotMember
	}
	return err
}
