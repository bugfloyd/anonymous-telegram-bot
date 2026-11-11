package directory

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/incognitochat/incognitochat/backend/internal/links"
)

const (
	linkPrefix = "K#"
	skLink     = "L"
)

// ErrLimit is returned when an account already has the most links allowed.
var ErrLimit = errors.New("directory: limit reached")

// Link is a link's public bundle.
type Link struct {
	ID         string
	SigningKey []byte
	Keys       []links.Key
}

func linkPK(id string) string { return "L#" + id }

// CreateLink stores a new link for an account, counting it against max. It
// returns ErrConflict if the link ID is taken, and ErrLimit if the account
// has max links already (or no longer exists).
func (s *Store) CreateLink(ctx context.Context, accountID string, l Link, max int) error {
	writes := []types.TransactWriteItem{
		{Put: &types.Put{
			TableName: &s.Table,
			Item: item(linkPK(l.ID), skLink, attrs{
				"a":  sv(accountID),
				"k":  bv(l.SigningKey),
				"ek": keysValue(l.Keys),
			}),
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		}},
		{Put: &types.Put{
			TableName: &s.Table,
			Item:      item(accountPK(accountID), linkPrefix+l.ID, attrs{}),
		}},
		{Update: &types.Update{
			TableName:           &s.Table,
			Key:                 key(accountPK(accountID), skAccount),
			UpdateExpression:    aws.String("SET lc = if_not_exists(lc, :zero) + :one"),
			ConditionExpression: aws.String("attribute_exists(ak) AND (attribute_not_exists(lc) OR lc < :max)"),
			ExpressionAttributeValues: attrs{
				":zero": nv(0),
				":one":  nv(1),
				":max":  nv(int64(max)),
			},
		}},
	}
	reasons, err := s.transactReasons(ctx, writes)
	if errors.Is(err, ErrConflict) && len(reasons) == 3 && reasons[2] == "ConditionalCheckFailed" {
		return ErrLimit
	}
	return err
}

// Link returns a link's bundle. Revoked and unknown links are both ErrNotFound.
func (s *Store) Link(ctx context.Context, id string) (Link, error) {
	it, err := s.get(ctx, linkPK(id), skLink, time.Time{})
	if err != nil {
		return Link{}, err
	}
	k, ok := bin(it, "k")
	keys, okKeys := keysFrom(it["ek"])
	if !ok || !okKeys {
		return Link{}, ErrNotFound
	}
	return Link{ID: id, SigningKey: k, Keys: keys}, nil
}

// SetLinkKeys replaces a link's encryption keys. It returns ErrNotFound unless
// the account owns the link.
func (s *Store) SetLinkKeys(ctx context.Context, accountID, id string, keys []links.Key) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Table,
		Key:                       key(linkPK(id), skLink),
		UpdateExpression:          aws.String("SET ek = :ek"),
		ConditionExpression:       aws.String("a = :a"),
		ExpressionAttributeValues: attrs{":ek": keysValue(keys), ":a": sv(accountID)},
	})
	var failed *types.ConditionalCheckFailedException
	if errors.As(err, &failed) {
		return ErrNotFound
	}
	return err
}

// DeleteLink revokes a link. It returns ErrNotFound unless the account owns it.
func (s *Store) DeleteLink(ctx context.Context, accountID, id string) error {
	writes := []types.TransactWriteItem{
		{Delete: &types.Delete{
			TableName:                 &s.Table,
			Key:                       key(linkPK(id), skLink),
			ConditionExpression:       aws.String("a = :a"),
			ExpressionAttributeValues: attrs{":a": sv(accountID)},
		}},
		{Delete: &types.Delete{
			TableName: &s.Table,
			Key:       key(accountPK(accountID), linkPrefix+id),
		}},
		{Update: &types.Update{
			TableName:                 &s.Table,
			Key:                       key(accountPK(accountID), skAccount),
			UpdateExpression:          aws.String("SET lc = lc - :one"),
			ConditionExpression:       aws.String("lc > :zero"),
			ExpressionAttributeValues: attrs{":zero": nv(0), ":one": nv(1)},
		}},
	}
	reasons, err := s.transactReasons(ctx, writes)
	if errors.Is(err, ErrConflict) && len(reasons) > 0 && reasons[0] == "ConditionalCheckFailed" {
		return ErrNotFound
	}
	return err
}

func keysValue(keys []links.Key) types.AttributeValue {
	list := make([]types.AttributeValue, len(keys))
	for i, k := range keys {
		list[i] = &types.AttributeValueMemberM{Value: attrs{
			"k": bv(k.Key),
			"f": nv(k.From.Unix()),
			"u": nv(k.Until.Unix()),
			"s": bv(k.Signature),
		}}
	}
	return &types.AttributeValueMemberL{Value: list}
}

func keysFrom(v types.AttributeValue) ([]links.Key, bool) {
	list, ok := v.(*types.AttributeValueMemberL)
	if !ok {
		return nil, false
	}
	keys := make([]links.Key, 0, len(list.Value))
	for _, e := range list.Value {
		m, ok := e.(*types.AttributeValueMemberM)
		if !ok {
			return nil, false
		}
		k, ok1 := bin(m.Value, "k")
		sig, ok2 := bin(m.Value, "s")
		from, ok3 := m.Value["f"].(*types.AttributeValueMemberN)
		until, ok4 := m.Value["u"].(*types.AttributeValueMemberN)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return nil, false
		}
		f, err1 := strconv.ParseInt(from.Value, 10, 64)
		u, err2 := strconv.ParseInt(until.Value, 10, 64)
		if err1 != nil || err2 != nil {
			return nil, false
		}
		keys = append(keys, links.Key{Key: k, From: time.Unix(f, 0), Until: time.Unix(u, 0), Signature: sig})
	}
	return keys, true
}
