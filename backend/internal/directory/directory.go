// Package directory stores accounts and their devices in DynamoDB.
//
// Everything an account owns lives in one partition, "A#<account ID>", so
// replacing or deleting an account can find all of it:
//
//	A#<account>  A          account: account key (ak), Telegram hash (th)
//	A#<account>  D#<device> device: signing key (k)
//	A#<account>  L          signed device list: bytes (b), signature (s), sequence (q)
//	A#<account>  K#<link>   marks a link the account owns (links.go)
//	L#<link>     L          a link: owner (a), signing key (k), encryption keys (ek)
//	T#<hash>     T          one account per Telegram user: account ID (a)
//
// The account item also counts the account's active links (lc) and, once it
// joined through an invitation, records its level (lv).
//
// The Telegram hash is a keyed hash of the Telegram user ID (TelegramHash),
// so the table enforces one account per user without holding the ID.
//
// A replaced account keeps a marker (g = "r") and its device keys until the
// marker expires, so its devices can be told what happened after checking
// their signatures. Its other items are deleted. Times are stored rounded up
// to the hour (attribute e, the table's TTL).
package directory

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
)

// ReplacedTTL is how long a replaced account's devices are told so.
const ReplacedTTL = 90 * 24 * time.Hour

const (
	skAccount    = "A"
	skDeviceList = "L"
	skTelegram   = "T"
	devicePrefix = "D#"
	goneReplaced = "r"

	// labelTelegram keys the hash of Telegram user IDs.
	labelTelegram = "incognitochat/v2/telegram-id"
	// batchSize is DynamoDB's limit for BatchWriteItem.
	batchSize = 25
	// maxBatchRetries bounds retries of unprocessed batch items.
	maxBatchRetries = 5
)

var (
	// ErrNotFound is returned when there is no such account or device.
	ErrNotFound = errors.New("directory: not found")
	// ErrConflict is returned when a concurrent change won.
	ErrConflict = errors.New("directory: conflict")
)

// DB is the part of the DynamoDB client the store uses.
type DB interface {
	GetItem(context.Context, *dynamodb.GetItemInput, ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	Query(context.Context, *dynamodb.QueryInput, ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	UpdateItem(context.Context, *dynamodb.UpdateItemInput, ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	TransactWriteItems(context.Context, *dynamodb.TransactWriteItemsInput, ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
	BatchWriteItem(context.Context, *dynamodb.BatchWriteItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

// Store is the directory table.
type Store struct {
	DB    DB
	Table string
}

// TelegramHash is the keyed hash a Telegram user ID is stored as.
func TelegramHash(pepper []byte, userID int64) string {
	mac := hmac.New(sha256.New, pepper)
	mac.Write(devicesig.Message(labelTelegram, []byte(strconv.FormatInt(userID, 10))))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Account is an existing account, as registration needs it.
type Account struct {
	ID      string
	Key     []byte            // account key; nil if the account item is missing
	Devices map[string][]byte // device ID -> signing key
	items   []string          // every sort key in the partition
}

// NewAccount is a registration to store.
type NewAccount struct {
	ID            string
	TelegramHash  string
	AccountKey    []byte
	DeviceID      string
	DeviceKey     []byte
	DeviceList    []byte
	DeviceListSig []byte
	DeviceListSeq uint64
}

// Device is a device's signing key, with what requests need to know about
// its account.
type Device struct {
	Key          []byte
	Replaced     bool
	TelegramHash string // "" once replaced
	Member       bool   // joined through an invitation
	Links        int    // active links
}

// ByTelegram returns the account registered for a Telegram hash.
func (s *Store) ByTelegram(ctx context.Context, telegramHash string) (Account, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      &s.Table,
		Key:            key("T#"+telegramHash, skTelegram),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return Account{}, err
	}
	id, ok := str(out.Item, "a")
	if !ok {
		return Account{}, ErrNotFound
	}

	acct := Account{ID: id, Devices: map[string][]byte{}}
	pages := dynamodb.NewQueryPaginator(s.DB, &dynamodb.QueryInput{
		TableName:              &s.Table,
		KeyConditionExpression: aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: accountPK(id)},
		},
		ConsistentRead: aws.Bool(true),
	})
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return Account{}, err
		}
		for _, it := range page.Items {
			sk, _ := str(it, "sk")
			acct.items = append(acct.items, sk)
			switch {
			case sk == skAccount:
				if _, gone := str(it, "g"); !gone {
					acct.Key, _ = bin(it, "ak")
				}
			case strings.HasPrefix(sk, devicePrefix):
				if k, ok := bin(it, "k"); ok {
					acct.Devices[strings.TrimPrefix(sk, devicePrefix)] = k
				}
			}
		}
	}
	return acct, nil
}

// Create stores a new account. It returns ErrConflict if the Telegram user
// already has one.
func (s *Store) Create(ctx context.Context, n NewAccount) error {
	writes := append([]types.TransactWriteItem{{
		Put: &types.Put{
			TableName:           &s.Table,
			Item:                item("T#"+n.TelegramHash, skTelegram, attrs{"a": sv(n.ID)}),
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		},
	}}, s.newAccountItems(n)...)
	return s.transact(ctx, writes)
}

// Replace ends old and stores n in its place, atomically. It returns
// ErrConflict if the Telegram user's account changed since old was read.
func (s *Store) Replace(ctx context.Context, old Account, n NewAccount, now time.Time) error {
	expires := roundUp(now.Add(ReplacedTTL))
	writes := []types.TransactWriteItem{{
		Update: &types.Update{
			TableName:           &s.Table,
			Key:                 key("T#"+n.TelegramHash, skTelegram),
			UpdateExpression:    aws.String("SET a = :new"),
			ConditionExpression: aws.String("a = :old"),
			ExpressionAttributeValues: map[string]types.AttributeValue{
				":new": sv(n.ID),
				":old": sv(old.ID),
			},
		},
	}}
	writes = append(writes, s.newAccountItems(n)...)
	// The old account ends in the same transaction.
	writes = append(writes, types.TransactWriteItem{Put: &types.Put{
		TableName: &s.Table,
		Item:      item(accountPK(old.ID), skAccount, attrs{"g": sv(goneReplaced), "e": nv(expires)}),
	}})
	if err := s.transact(ctx, writes); err != nil {
		return err
	}

	// Then the rest of its partition: device keys stay until the marker
	// expires, everything else goes now.
	var requests []types.WriteRequest
	for _, sk := range old.items {
		switch {
		case sk == skAccount:
		case strings.HasPrefix(sk, linkPrefix):
			// The link itself goes too: its bundle then answers "not found".
			requests = append(requests,
				types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(accountPK(old.ID), sk)}},
				types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(linkPK(strings.TrimPrefix(sk, linkPrefix)), skLink)}},
			)
		case strings.HasPrefix(sk, devicePrefix):
			k, ok := old.Devices[strings.TrimPrefix(sk, devicePrefix)]
			if !ok {
				continue
			}
			requests = append(requests, types.WriteRequest{PutRequest: &types.PutRequest{
				Item: item(accountPK(old.ID), sk, attrs{"k": bv(k), "e": nv(expires)}),
			}})
		default:
			requests = append(requests, types.WriteRequest{DeleteRequest: &types.DeleteRequest{
				Key: key(accountPK(old.ID), sk),
			}})
		}
	}
	return s.batchWrite(ctx, requests)
}

// Device returns a device's signing key, and whether its account was
// replaced. It returns ErrNotFound if the device or its account is gone.
func (s *Store) Device(ctx context.Context, accountID, deviceID string, now time.Time) (Device, error) {
	dev, err := s.get(ctx, accountPK(accountID), devicePrefix+deviceID, now)
	if err != nil {
		return Device{}, err
	}
	acct, err := s.get(ctx, accountPK(accountID), skAccount, now)
	if err != nil {
		return Device{}, err
	}
	k, ok := bin(dev, "k")
	if !ok {
		return Device{}, ErrNotFound
	}
	g, _ := str(acct, "g")
	th, _ := str(acct, "th")
	_, member := acct["lv"].(*types.AttributeValueMemberN)
	return Device{Key: k, Replaced: g == goneReplaced, TelegramHash: th, Member: member, Links: int(num(acct, "lc"))}, nil
}

func (s *Store) newAccountItems(n NewAccount) []types.TransactWriteItem {
	pk := accountPK(n.ID)
	return []types.TransactWriteItem{
		{Put: &types.Put{
			TableName:           &s.Table,
			Item:                item(pk, skAccount, attrs{"ak": bv(n.AccountKey), "th": sv(n.TelegramHash)}),
			ConditionExpression: aws.String("attribute_not_exists(pk)"),
		}},
		{Put: &types.Put{
			TableName: &s.Table,
			Item:      item(pk, devicePrefix+n.DeviceID, attrs{"k": bv(n.DeviceKey)}),
		}},
		{Put: &types.Put{
			TableName: &s.Table,
			Item: item(pk, skDeviceList, attrs{
				"b": bv(n.DeviceList), "s": bv(n.DeviceListSig), "q": nv(int64(n.DeviceListSeq)),
			}),
		}},
	}
}

// get reads one item, treating an expired item as missing: TTL deletion can
// lag by days.
func (s *Store) get(ctx context.Context, pk, sk string, now time.Time) (map[string]types.AttributeValue, error) {
	out, err := s.DB.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      &s.Table,
		Key:            key(pk, sk),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return nil, err
	}
	if out.Item == nil || expired(out.Item, now) {
		return nil, ErrNotFound
	}
	return out.Item, nil
}

func (s *Store) transact(ctx context.Context, writes []types.TransactWriteItem) error {
	_, err := s.transactReasons(ctx, writes)
	return err
}

// transactReasons runs a transaction. When it is cancelled because a
// condition failed or a concurrent transaction won, it returns ErrConflict
// and each item's cancellation code ("None" for items that were fine).
func (s *Store) transactReasons(ctx context.Context, writes []types.TransactWriteItem) ([]string, error) {
	_, err := s.DB.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: writes})
	var canceled *types.TransactionCanceledException
	if errors.As(err, &canceled) {
		reasons := make([]string, len(canceled.CancellationReasons))
		conflict := false
		for i, r := range canceled.CancellationReasons {
			reasons[i] = aws.ToString(r.Code)
			conflict = conflict || reasons[i] == "ConditionalCheckFailed" || reasons[i] == "TransactionConflict"
		}
		if conflict {
			return reasons, ErrConflict
		}
	}
	return nil, err
}

func (s *Store) batchWrite(ctx context.Context, requests []types.WriteRequest) error {
	for len(requests) > 0 {
		n := min(batchSize, len(requests))
		pending := map[string][]types.WriteRequest{s.Table: requests[:n]}
		requests = requests[n:]
		for attempt := 0; len(pending[s.Table]) > 0; attempt++ {
			if attempt == maxBatchRetries {
				return errors.New("directory: batch write left items unprocessed")
			}
			if attempt > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(time.Duration(50<<attempt) * time.Millisecond):
				}
			}
			out, err := s.DB.BatchWriteItem(ctx, &dynamodb.BatchWriteItemInput{RequestItems: pending})
			if err != nil {
				return err
			}
			pending = out.UnprocessedItems
		}
	}
	return nil
}

func accountPK(id string) string { return "A#" + id }

func key(pk, sk string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"pk": sv(pk), "sk": sv(sk)}
}

type attrs = map[string]types.AttributeValue

// item is a whole item: its key plus other attributes.
func item(pk, sk string, a attrs) attrs {
	a["pk"] = sv(pk)
	a["sk"] = sv(sk)
	return a
}

func sv(v string) types.AttributeValue { return &types.AttributeValueMemberS{Value: v} }
func bv(v []byte) types.AttributeValue { return &types.AttributeValueMemberB{Value: v} }
func nv(v int64) types.AttributeValue {
	return &types.AttributeValueMemberN{Value: strconv.FormatInt(v, 10)}
}

func str(item map[string]types.AttributeValue, name string) (string, bool) {
	v, ok := item[name].(*types.AttributeValueMemberS)
	if !ok {
		return "", false
	}
	return v.Value, true
}

func bin(item map[string]types.AttributeValue, name string) ([]byte, bool) {
	v, ok := item[name].(*types.AttributeValueMemberB)
	if !ok {
		return nil, false
	}
	return v.Value, true
}

func num(item map[string]types.AttributeValue, name string) int64 {
	v, ok := item[name].(*types.AttributeValueMemberN)
	if !ok {
		return 0
	}
	n, _ := strconv.ParseInt(v.Value, 10, 64)
	return n
}

func expired(item map[string]types.AttributeValue, now time.Time) bool {
	v, ok := item["e"].(*types.AttributeValueMemberN)
	if !ok {
		return false
	}
	e, err := strconv.ParseInt(v.Value, 10, 64)
	return err != nil || e <= now.Unix()
}

// roundUp returns t rounded up to the next whole hour, in Unix seconds.
func roundUp(t time.Time) int64 {
	return t.Add(time.Hour - time.Nanosecond).Truncate(time.Hour).Unix()
}
