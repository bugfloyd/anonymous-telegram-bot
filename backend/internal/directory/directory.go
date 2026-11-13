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
//	A#<account>  I#<code>   marks an invitation code the account made (invitations.go)
//	I#<code>     I          a code: owner (o), owner's level (l), uses (u), uses left (r)
//	S#levels     S          invitation allowance per level (m), version (v)
//	M#<mailbox>  M          a mailbox (mailboxes.go)
//	T#<hash>     T          one account per Telegram user: account ID (a)
//
// The account item also counts the account's active links (lc) and, once it
// joined through an invitation, records its level (lv), the invitations
// granted to it on top of its level's allowance (ig) and the uses it has put
// into codes (ia).
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
	BatchGetItem(context.Context, *dynamodb.BatchGetItemInput, ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error)
	PutItem(context.Context, *dynamodb.PutItemInput, ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
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
	Level        int    // 1 for people invited by an admin, and so on; 0 if not a member
	Granted      int    // invitations granted on top of the level's allowance
	Allocated    int    // invitation uses put into codes
	Links        int    // active links
	Notices      bool   // Telegram notices are set up and not turned off
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
		case strings.HasPrefix(sk, mailboxPrefix):
			// Sends to the account's mailboxes then go nowhere.
			requests = append(requests,
				types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(accountPK(old.ID), sk)}},
				types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(mailboxPK(strings.TrimPrefix(sk, mailboxPrefix)), skMailbox)}},
			)
		case strings.HasPrefix(sk, codePrefix):
			// Unused codes of an ended account stop working.
			requests = append(requests,
				types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(accountPK(old.ID), sk)}},
				types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(codePK(strings.TrimPrefix(sk, codePrefix)), skCode)}},
			)
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

// Delete removes an account and everything it owns, keeping nothing (unlike
// Replace): its links (whose bundles then answer "not found"), mailboxes
// (sends to them then go nowhere), invitation codes, device list, devices,
// the account item, and the Telegram item, so the same Telegram user can set
// up again later. It returns ErrNotFound if the account is already gone or
// replaced.
//
// The order lets a failed delete be retried by the same device: everything
// but the account and its devices goes first, then the account and the
// Telegram item together, then the devices (orphaned devices can't log in).
func (s *Store) Delete(ctx context.Context, accountID, telegramHash string) error {
	sks, err := s.sortKeys(ctx, accountID)
	if err != nil {
		return err
	}
	var owned, devices []types.WriteRequest
	del := func(pk, sk string) types.WriteRequest {
		return types.WriteRequest{DeleteRequest: &types.DeleteRequest{Key: key(pk, sk)}}
	}
	for _, sk := range sks {
		switch {
		case sk == skAccount:
		case strings.HasPrefix(sk, devicePrefix):
			devices = append(devices, del(accountPK(accountID), sk))
		case strings.HasPrefix(sk, mailboxPrefix):
			owned = append(owned, del(mailboxPK(strings.TrimPrefix(sk, mailboxPrefix)), skMailbox), del(accountPK(accountID), sk))
		case strings.HasPrefix(sk, codePrefix):
			owned = append(owned, del(codePK(strings.TrimPrefix(sk, codePrefix)), skCode), del(accountPK(accountID), sk))
		case strings.HasPrefix(sk, linkPrefix):
			owned = append(owned, del(linkPK(strings.TrimPrefix(sk, linkPrefix)), skLink), del(accountPK(accountID), sk))
		default:
			owned = append(owned, del(accountPK(accountID), sk))
		}
	}
	if err := s.batchWrite(ctx, owned); err != nil {
		return err
	}
	writes := []types.TransactWriteItem{{Delete: &types.Delete{
		TableName:           &s.Table,
		Key:                 key(accountPK(accountID), skAccount),
		ConditionExpression: aws.String("attribute_exists(pk) AND attribute_not_exists(g)"),
	}}}
	if telegramHash != "" {
		writes = append(writes, types.TransactWriteItem{Delete: &types.Delete{
			TableName:                 &s.Table,
			Key:                       key("T#"+telegramHash, skTelegram),
			ConditionExpression:       aws.String("a = :id"),
			ExpressionAttributeValues: attrs{":id": sv(accountID)},
		}})
	}
	if err := s.transact(ctx, writes); err != nil {
		if errors.Is(err, ErrConflict) {
			return ErrNotFound
		}
		return err
	}
	return s.batchWrite(ctx, devices)
}

// sortKeys lists every item in an account's partition.
func (s *Store) sortKeys(ctx context.Context, accountID string) ([]string, error) {
	pages := dynamodb.NewQueryPaginator(s.DB, &dynamodb.QueryInput{
		TableName:                 &s.Table,
		KeyConditionExpression:    aws.String("pk = :pk"),
		ExpressionAttributeValues: attrs{":pk": sv(accountPK(accountID))},
		ProjectionExpression:      aws.String("sk"),
		ConsistentRead:            aws.Bool(true),
	})
	var sks []string
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, it := range page.Items {
			if sk, ok := str(it, "sk"); ok {
				sks = append(sks, sk)
			}
		}
	}
	return sks, nil
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
	_, chat := acct["cid"]
	_, off := acct["no"]
	return Device{
		Key:          k,
		Replaced:     g == goneReplaced,
		TelegramHash: th,
		Member:       member,
		Level:        int(num(acct, "lv")),
		Granted:      int(num(acct, "ig")),
		Allocated:    int(num(acct, "ia")),
		Links:        int(num(acct, "lc")),
		Notices:      chat && !off,
	}, nil
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

// Notices are the Telegram "New message" notices (design §12). The account
// item keeps the chat ID sealed by the notify function (cid), which nobody
// else can read; the language for the notice (lg); the last notice's message
// ID, to delete it when a new one is sent (nm); and whether Telegram refused
// the last notice, meaning notices are off for this user (no).

// NoticeTarget is what the notify function needs about an account.
type NoticeTarget struct {
	SealedChat  []byte
	Language    string
	LastMessage int64
	Off         bool
}

// SetNotices stores an account's sealed chat ID and language, and turns
// notices back on.
func (s *Store) SetNotices(ctx context.Context, accountID string, sealedChat []byte, language string) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Table,
		Key:                       key(accountPK(accountID), skAccount),
		UpdateExpression:          aws.String("SET cid = :c, lg = :l REMOVE #no"),
		ConditionExpression:       aws.String("attribute_exists(ak)"),
		ExpressionAttributeNames:  map[string]string{"#no": "no"},
		ExpressionAttributeValues: attrs{":c": bv(sealedChat), ":l": sv(language)},
	})
	var failed *types.ConditionalCheckFailedException
	if errors.As(err, &failed) {
		return ErrNotFound
	}
	return err
}

// Notices returns an account's notice settings. ErrNotFound if it has none.
func (s *Store) Notices(ctx context.Context, accountID string) (NoticeTarget, error) {
	it, err := s.get(ctx, accountPK(accountID), skAccount, time.Time{})
	if err != nil {
		return NoticeTarget{}, err
	}
	sealed, ok := bin(it, "cid")
	if !ok {
		return NoticeTarget{}, ErrNotFound
	}
	lang, _ := str(it, "lg")
	_, off := it["no"]
	return NoticeTarget{SealedChat: sealed, Language: lang, LastMessage: num(it, "nm"), Off: off}, nil
}

// NoticeSent records the message ID of the latest notice.
func (s *Store) NoticeSent(ctx context.Context, accountID string, messageID int64) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Table,
		Key:                       key(accountPK(accountID), skAccount),
		UpdateExpression:          aws.String("SET nm = :m"),
		ConditionExpression:       aws.String("attribute_exists(ak)"),
		ExpressionAttributeValues: attrs{":m": nv(messageID)},
	})
	var failed *types.ConditionalCheckFailedException
	if errors.As(err, &failed) {
		return nil // the account is gone
	}
	return err
}

// NoticesOff records that Telegram refused a notice (the user blocked the
// bot or never let it write), so none are tried until the app turns them on again.
func (s *Store) NoticesOff(ctx context.Context, accountID string) error {
	_, err := s.DB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &s.Table,
		Key:                       key(accountPK(accountID), skAccount),
		UpdateExpression:          aws.String("SET #no = :one"),
		ConditionExpression:       aws.String("attribute_exists(ak)"),
		ExpressionAttributeNames:  map[string]string{"#no": "no"},
		ExpressionAttributeValues: attrs{":one": nv(1)},
	})
	var failed *types.ConditionalCheckFailedException
	if errors.As(err, &failed) {
		return nil
	}
	return err
}
