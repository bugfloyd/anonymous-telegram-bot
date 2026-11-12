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
	Mailbox    string // the intro mailbox first messages go to
}

func linkPK(id string) string { return "L#" + id }

// CreateLink stores a new link for an account, counting it against max. It
// returns ErrConflict if the link ID is taken, and ErrLimit if the account
// has max links already (or no longer exists).
func (s *Store) CreateLink(ctx context.Context, accountID string, l Link, max int) error {
	writes := append(s.newLinkWrites(accountID, l), types.TransactWriteItem{Update: &types.Update{
		TableName:           &s.Table,
		Key:                 key(accountPK(accountID), skAccount),
		UpdateExpression:    aws.String("SET lc = if_not_exists(lc, :zero) + :one"),
		ConditionExpression: aws.String("attribute_exists(ak) AND (attribute_not_exists(lc) OR lc < :max)"),
		ExpressionAttributeValues: attrs{
			":zero": nv(0),
			":one":  nv(1),
			":max":  nv(int64(max)),
		},
	}})
	writes = append(writes, s.introMailboxWrites(accountID, l.ID, l.Mailbox)...)
	reasons, err := s.transactReasons(ctx, writes)
	if errors.Is(err, ErrConflict) && len(reasons) == len(writes) && reasons[2] == "ConditionalCheckFailed" {
		return ErrLimit
	}
	return err
}

// newLinkWrites stores a link's bundle and lists it under its account.
func (s *Store) newLinkWrites(accountID string, l Link) []types.TransactWriteItem {
	return []types.TransactWriteItem{
		{Put: &types.Put{
			TableName: &s.Table,
			Item: item(linkPK(l.ID), skLink, attrs{
				"a":  sv(accountID),
				"k":  bv(l.SigningKey),
				"ek": keysValue(l.Keys),
				"m":  sv(l.Mailbox),
			}),
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		}},
		{Put: &types.Put{
			TableName: &s.Table,
			Item:      item(accountPK(accountID), linkPrefix+l.ID, attrs{}),
		}},
	}
}

// oldLinkWrites deletes an account's link, its listing and its intro mailbox.
// The first write fails its condition unless the account owns the link.
func (s *Store) oldLinkWrites(accountID string, old Link) []types.TransactWriteItem {
	return []types.TransactWriteItem{
		{Delete: &types.Delete{
			TableName:                 &s.Table,
			Key:                       key(linkPK(old.ID), skLink),
			ConditionExpression:       aws.String("a = :a"),
			ExpressionAttributeValues: attrs{":a": sv(accountID)},
		}},
		{Delete: &types.Delete{TableName: &s.Table, Key: key(accountPK(accountID), linkPrefix+old.ID)}},
		{Delete: &types.Delete{TableName: &s.Table, Key: key(mailboxPK(old.Mailbox), skMailbox)}},
		{Delete: &types.Delete{TableName: &s.Table, Key: key(accountPK(accountID), mailboxPrefix+old.Mailbox)}},
	}
}

// ReplaceLink swaps an account's link for a new one in one step: the old
// link and its intro mailbox go, the new one takes its place, and the link
// count stays the same. It returns ErrNotFound unless the account owns oldID,
// and ErrConflict if the new ID is taken.
func (s *Store) ReplaceLink(ctx context.Context, accountID, oldID string, l Link) error {
	if oldID == l.ID {
		return ErrConflict
	}
	old, err := s.Link(ctx, oldID)
	if err != nil {
		return err
	}
	writes := append(s.oldLinkWrites(accountID, old), s.newLinkWrites(accountID, l)...)
	writes = append(writes, s.introMailboxWrites(accountID, l.ID, l.Mailbox)...)
	reasons, err := s.transactReasons(ctx, writes)
	if errors.Is(err, ErrConflict) && len(reasons) > 0 && reasons[0] == "ConditionalCheckFailed" {
		return ErrNotFound
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
	mailbox, okMailbox := str(it, "m")
	if !ok || !okKeys || !okMailbox {
		return Link{}, ErrNotFound
	}
	return Link{ID: id, SigningKey: k, Keys: keys, Mailbox: mailbox}, nil
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

// DeleteLink revokes a link and its intro mailbox. It returns ErrNotFound
// unless the account owns it.
func (s *Store) DeleteLink(ctx context.Context, accountID, id string) error {
	l, err := s.Link(ctx, id)
	if err != nil {
		return err
	}
	writes := append(s.oldLinkWrites(accountID, l), types.TransactWriteItem{Update: &types.Update{
		TableName:                 &s.Table,
		Key:                       key(accountPK(accountID), skAccount),
		UpdateExpression:          aws.String("SET lc = lc - :one"),
		ConditionExpression:       aws.String("lc > :zero"),
		ExpressionAttributeValues: attrs{":zero": nv(0), ":one": nv(1)},
	}})
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
