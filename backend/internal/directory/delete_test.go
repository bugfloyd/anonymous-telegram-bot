package directory

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/links"
)

func TestDelete(t *testing.T) {
	s, client := store(t)
	ctx := context.Background()
	other := registered(t, s)
	n := registered(t, s)
	l := testLink(t)
	pool := []string{ids.New(), ids.New()}
	if err := s.CreateLink(ctx, n.ID, l, links.MaxActive); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMailboxes(ctx, n.ID, pool, 10); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInvitation(ctx, n.ID, "abcd-efgh", 1, 0, -1); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNotices(ctx, n.ID, []byte("sealed chat"), "en"); err != nil {
		t.Fatal(err)
	}

	if err := s.Delete(ctx, n.ID, n.TelegramHash); err != nil {
		t.Fatal(err)
	}

	// Nothing of it is left: the table holds only the other account's items.
	out, err := client.Scan(ctx, &dynamodb.ScanInput{TableName: aws.String(s.Table)})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range out.Items {
		pk, _ := str(it, "pk")
		if pk != accountPK(other.ID) && pk != "T#"+other.TelegramHash {
			sk, _ := str(it, "sk")
			t.Errorf("left behind: %s %s", pk, sk)
		}
	}
	if _, err := s.Device(ctx, n.ID, n.DeviceID, now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("device of a deleted account: %v", err)
	}
	if _, err := s.Link(ctx, l.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("link of a deleted account: %v", err)
	}
	for _, id := range append(pool, l.Mailbox) {
		if _, err := s.Mailbox(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("mailbox %s of a deleted account: %v", id, err)
		}
	}
	if _, err := s.Redeem(ctx, other.ID, "abcd-efgh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("code of a deleted account: %v", err)
	}

	// The same Telegram user can set up again, as new.
	if err := s.Create(ctx, newAccount(t, n.TelegramHash)); err != nil {
		t.Fatalf("set up again: %v", err)
	}
	// Deleting again finds nothing.
	if err := s.Delete(ctx, n.ID, n.TelegramHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
}

func TestDeleteReplacedAccount(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	first := registered(t, s)
	old, _ := s.ByTelegram(ctx, first.TelegramHash)
	second := newAccount(t, first.TelegramHash)
	if err := s.Replace(ctx, old, second, now); err != nil {
		t.Fatal(err)
	}
	// A replaced account can't delete anything, least of all its successor's Telegram item.
	if err := s.Delete(ctx, first.ID, first.TelegramHash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete a replaced account: %v", err)
	}
	if got, err := s.ByTelegram(ctx, first.TelegramHash); err != nil || got.ID != second.ID {
		t.Fatalf("successor = %+v, %v", got, err)
	}
}
