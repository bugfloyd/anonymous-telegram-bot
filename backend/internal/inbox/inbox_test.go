package inbox

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/dynamotest"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

var now = time.Unix(1_800_000_000, 0)

func TestDeliverListAck(t *testing.T) {
	client := dynamotest.Client(t)
	s := &Store{DB: client, Table: dynamotest.Table(t, client)}
	ctx := context.Background()
	account, phone, laptop, mailbox := ids.New(), ids.New(), ids.New(), ids.New()

	for i := range 3 {
		if err := s.Deliver(ctx, account, []string{phone, laptop}, mailbox, []byte{1, byte(i)}, now); err != nil {
			t.Fatal(err)
		}
	}
	items, more, err := s.List(ctx, account, phone, 2, now)
	if err != nil || len(items) != 2 || !more || items[0].Mailbox != mailbox {
		t.Fatalf("List = %+v, %v, %v", items, more, err)
	}
	all, more, _ := s.List(ctx, account, phone, 50, now)
	if len(all) != 3 || more {
		t.Fatalf("phone has %d items, more %v", len(all), more)
	}

	// Confirming on the phone leaves the laptop's copies alone.
	if err := s.Ack(ctx, account, phone, []string{all[0].ID, all[1].ID, ids.New()}); err != nil {
		t.Fatal(err)
	}
	if left, _, _ := s.List(ctx, account, phone, 50, now); len(left) != 1 || !bytes.Equal(left[0].Envelope, all[2].Envelope) {
		t.Fatalf("phone after ack: %+v", left)
	}
	if laptopItems, _, _ := s.List(ctx, account, laptop, 50, now); len(laptopItems) != 3 {
		t.Fatalf("laptop has %d items", len(laptopItems))
	}

	// The same device ID under another account sees nothing.
	if other, _, _ := s.List(ctx, ids.New(), phone, 50, now); len(other) != 0 {
		t.Fatalf("another account sees %d items", len(other))
	}
	// Expired items are hidden even before TTL deletes them.
	if late, _, _ := s.List(ctx, account, laptop, 50, now.Add(TTL+2*time.Hour)); len(late) != 0 {
		t.Fatalf("%d expired items listed", len(late))
	}
}

func TestClear(t *testing.T) {
	client := dynamotest.Client(t)
	s := &Store{DB: client, Table: dynamotest.Table(t, client)}
	ctx := context.Background()
	account, phone, other, mailbox := ids.New(), ids.New(), ids.New(), ids.New()

	// More than one batch of items.
	for i := range 30 {
		if err := s.Deliver(ctx, account, []string{phone}, mailbox, []byte{2, byte(i)}, now); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Deliver(ctx, other, []string{phone}, mailbox, []byte{3}, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Clear(ctx, account, phone); err != nil {
		t.Fatal(err)
	}
	if left, _, _ := s.List(ctx, account, phone, 50, now); len(left) != 0 {
		t.Fatalf("%d items left", len(left))
	}
	// Another account's device of the same ID keeps its items.
	if kept, _, _ := s.List(ctx, other, phone, 50, now); len(kept) != 1 {
		t.Fatalf("other account has %d items", len(kept))
	}
}
