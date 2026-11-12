package directory

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/incognitochat/incognitochat/backend/internal/dynamotest"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

var now = time.Unix(1_800_000_000, 0)

func publicKey(t *testing.T) []byte {
	t.Helper()
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	b, err := k.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newAccount(t *testing.T, telegramHash string) NewAccount {
	return NewAccount{
		ID:            ids.New(),
		TelegramHash:  telegramHash,
		AccountKey:    publicKey(t),
		DeviceID:      ids.New(),
		DeviceKey:     publicKey(t),
		DeviceList:    []byte("list"),
		DeviceListSig: []byte("sig"),
		DeviceListSeq: 1,
	}
}

func store(t *testing.T) (*Store, *dynamodb.Client) {
	client := dynamotest.Client(t)
	return &Store{DB: client, Table: dynamotest.Table(t, client)}, client
}

func TestTelegramHash(t *testing.T) {
	a := TelegramHash([]byte("pepper-1"), 42)
	if a == TelegramHash([]byte("pepper-2"), 42) || a == TelegramHash([]byte("pepper-1"), 43) {
		t.Fatal("hash ignores its inputs")
	}
	if a != TelegramHash([]byte("pepper-1"), 42) || len(a) != 43 {
		t.Fatalf("hash = %q", a)
	}
}

func TestCreateAndRead(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()

	if _, err := s.ByTelegram(ctx, "th-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty table: %v", err)
	}
	n := newAccount(t, "th-1")
	if err := s.Create(ctx, n); err != nil {
		t.Fatal(err)
	}
	if err := s.Create(ctx, newAccount(t, "th-1")); !errors.Is(err, ErrConflict) {
		t.Fatalf("second account for one Telegram user: %v", err)
	}

	got, err := s.ByTelegram(ctx, "th-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != n.ID || !bytes.Equal(got.Key, n.AccountKey) || !bytes.Equal(got.Devices[n.DeviceID], n.DeviceKey) {
		t.Fatalf("ByTelegram = %+v", got)
	}

	dev, err := s.Device(ctx, n.ID, n.DeviceID, now)
	if err != nil || !bytes.Equal(dev.Key, n.DeviceKey) || dev.Replaced {
		t.Fatalf("Device = %+v, %v", dev, err)
	}
	if _, err := s.Device(ctx, n.ID, ids.New(), now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown device: %v", err)
	}
	if _, err := s.Device(ctx, ids.New(), n.DeviceID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("device under another account: %v", err)
	}
}

func TestReplace(t *testing.T) {
	s, client := store(t)
	ctx := context.Background()

	first := newAccount(t, "th-1")
	if err := s.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	old, err := s.ByTelegram(ctx, "th-1")
	if err != nil {
		t.Fatal(err)
	}

	second := newAccount(t, "th-1")
	at := now.Add(17 * time.Minute) // expiries round up to the next hour
	if err := s.Replace(ctx, old, second, at); err != nil {
		t.Fatal(err)
	}
	// A replace based on a stale read loses.
	if err := s.Replace(ctx, old, newAccount(t, "th-1"), now); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replace: %v", err)
	}

	got, err := s.ByTelegram(ctx, "th-1")
	if err != nil || got.ID != second.ID {
		t.Fatalf("ByTelegram after replace = %+v, %v", got, err)
	}

	// The old device can still prove itself, and learns it was replaced.
	dev, err := s.Device(ctx, first.ID, first.DeviceID, now)
	if err != nil || !dev.Replaced || !bytes.Equal(dev.Key, first.DeviceKey) {
		t.Fatalf("old device = %+v, %v", dev, err)
	}
	// Until the marker expires.
	if _, err := s.Device(ctx, first.ID, first.DeviceID, now.Add(ReplacedTTL+2*time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old device after expiry: %v", err)
	}

	// The old partition keeps only the marker and device keys, all expiring,
	// with no account key or Telegram hash.
	out, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              &s.Table,
		KeyConditionExpression: aws.String("pk = :pk"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":pk": &types.AttributeValueMemberS{Value: "A#" + first.ID},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 {
		t.Fatalf("old partition has %d items", len(out.Items))
	}
	wantExpiry := strconv.FormatInt(now.Add(ReplacedTTL+time.Hour).Unix(), 10)
	for _, it := range out.Items {
		if _, ok := it["ak"]; ok {
			t.Error("account key kept")
		}
		if _, ok := it["th"]; ok {
			t.Error("Telegram hash kept")
		}
		if e, ok := it["e"].(*types.AttributeValueMemberN); !ok || e.Value != wantExpiry {
			t.Errorf("expiry = %v, want %s", it["e"], wantExpiry)
		}
	}

	if dev, err := s.Device(ctx, second.ID, second.DeviceID, now); err != nil || dev.Replaced {
		t.Fatalf("new device = %+v, %v", dev, err)
	}
}

func TestRoundUp(t *testing.T) {
	for in, want := range map[int64]int64{3600: 3600, 3601: 7200, 7199: 7200, 0: 0} {
		if got := roundUp(time.Unix(in, 0)); got != want {
			t.Errorf("roundUp(%d) = %d, want %d", in, got, want)
		}
	}
}
