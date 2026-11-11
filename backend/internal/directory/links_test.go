package directory

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/links"
)

func linkID() string {
	return base64.RawURLEncoding.EncodeToString([]byte(ids.New())[:8])
}

func testLink(t *testing.T) Link {
	return Link{
		ID:         linkID(),
		SigningKey: publicKey(t),
		Keys: []links.Key{{
			Key:       publicKey(t),
			From:      now,
			Until:     now.Add(28 * 24 * time.Hour),
			Signature: bytes.Repeat([]byte{7}, 64),
		}},
	}
}

func registered(t *testing.T, s *Store) NewAccount {
	t.Helper()
	n := newAccount(t, "th-"+ids.New())
	if err := s.Create(context.Background(), n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestLinks(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	owner := registered(t, s)
	other := registered(t, s)

	l := testLink(t)
	if err := s.CreateLink(ctx, owner.ID, l, links.MaxActive); err != nil {
		t.Fatal(err)
	}
	got, err := s.Link(ctx, l.ID)
	if err != nil || !bytes.Equal(got.SigningKey, l.SigningKey) || len(got.Keys) != 1 ||
		!got.Keys[0].Until.Equal(l.Keys[0].Until) || !bytes.Equal(got.Keys[0].Signature, l.Keys[0].Signature) {
		t.Fatalf("Link = %+v, %v", got, err)
	}
	if err := s.CreateLink(ctx, other.ID, Link{ID: l.ID, SigningKey: publicKey(t), Keys: l.Keys}, links.MaxActive); !errors.Is(err, ErrConflict) {
		t.Fatalf("taken ID: %v", err)
	}
	if dev, _ := s.Device(ctx, owner.ID, owner.DeviceID, now); dev.Links != 1 || dev.TelegramHash != owner.TelegramHash || dev.Member {
		t.Fatalf("Device = %+v", dev)
	}

	// Only the owner can change or revoke it.
	if err := s.SetLinkKeys(ctx, other.ID, l.ID, l.Keys); !errors.Is(err, ErrNotFound) {
		t.Fatalf("SetLinkKeys by another account: %v", err)
	}
	if err := s.DeleteLink(ctx, other.ID, l.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeleteLink by another account: %v", err)
	}
	rotated := testLink(t).Keys
	if err := s.SetLinkKeys(ctx, owner.ID, l.ID, rotated); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Link(ctx, l.ID); !bytes.Equal(got.Keys[0].Key, rotated[0].Key) {
		t.Fatal("keys not replaced")
	}

	if err := s.DeleteLink(ctx, owner.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(ctx, l.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked link: %v", err)
	}
	if err := s.DeleteLink(ctx, owner.ID, l.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second revoke: %v", err)
	}
	if dev, _ := s.Device(ctx, owner.ID, owner.DeviceID, now); dev.Links != 0 {
		t.Fatalf("count after revoke = %d", dev.Links)
	}
}

func TestLinkLimit(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	owner := registered(t, s)
	for i := range links.MaxActive {
		if err := s.CreateLink(ctx, owner.ID, testLink(t), links.MaxActive); err != nil {
			t.Fatalf("link %d: %v", i+1, err)
		}
	}
	if err := s.CreateLink(ctx, owner.ID, testLink(t), links.MaxActive); !errors.Is(err, ErrLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	if err := s.CreateLink(ctx, ids.New(), testLink(t), links.MaxActive); !errors.Is(err, ErrLimit) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestReplaceRevokesLinks(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	first := registered(t, s)
	l := testLink(t)
	if err := s.CreateLink(ctx, first.ID, l, links.MaxActive); err != nil {
		t.Fatal(err)
	}
	old, err := s.ByTelegram(ctx, first.TelegramHash)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Replace(ctx, old, newAccount(t, first.TelegramHash), now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Link(ctx, l.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("link of a replaced account: %v", err)
	}
}
