package directory

import (
	"context"
	"errors"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/links"
)

func TestMailboxes(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	owner := registered(t, s)
	other := registered(t, s)

	pool := []string{ids.New(), ids.New(), ids.New()}
	if err := s.CreateMailboxes(ctx, owner.ID, pool, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMailboxes(ctx, owner.ID, []string{ids.New(), ids.New()}, 4); !errors.Is(err, ErrLimit) {
		t.Fatalf("over the limit: %v", err)
	}
	if err := s.CreateMailboxes(ctx, other.ID, []string{pool[0]}, 4); !errors.Is(err, ErrConflict) {
		t.Fatalf("taken ID: %v", err)
	}
	m, err := s.Mailbox(ctx, pool[0])
	if err != nil || m.Owner != owner.ID || m.Link != "" {
		t.Fatalf("Mailbox = %+v, %v", m, err)
	}

	// Discarding: only by the owner, then it's gone like an unknown one.
	if err := s.DiscardMailbox(ctx, other.ID, pool[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("discard by another account: %v", err)
	}
	if err := s.DiscardMailbox(ctx, owner.ID, pool[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Mailbox(ctx, pool[0]); !errors.Is(err, ErrNotFound) {
		t.Fatalf("discarded mailbox: %v", err)
	}
	if _, err := s.Mailbox(ctx, ids.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown mailbox: %v", err)
	}
	// The discard freed a place.
	if err := s.CreateMailboxes(ctx, owner.ID, []string{ids.New(), ids.New()}, 4); err != nil {
		t.Fatalf("after discard: %v", err)
	}

	devices, err := s.Devices(ctx, owner.ID, now)
	if err != nil || len(devices) != 1 || devices[0] != owner.DeviceID {
		t.Fatalf("Devices = %v, %v", devices, err)
	}
}

func TestIntroMailboxes(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	owner := registered(t, s)
	l := testLink(t)
	if err := s.CreateLink(ctx, owner.ID, l, links.MaxActive); err != nil {
		t.Fatal(err)
	}
	m, err := s.Mailbox(ctx, l.Mailbox)
	if err != nil || m.Owner != owner.ID || m.Link != l.ID {
		t.Fatalf("intro mailbox = %+v, %v", m, err)
	}
	// Intro mailboxes go with their link, not one by one.
	if err := s.DiscardMailbox(ctx, owner.ID, l.Mailbox); !errors.Is(err, ErrNotFound) {
		t.Fatalf("discarding an intro mailbox: %v", err)
	}
	if err := s.DeleteLink(ctx, owner.ID, l.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Mailbox(ctx, l.Mailbox); !errors.Is(err, ErrNotFound) {
		t.Fatalf("intro mailbox of a revoked link: %v", err)
	}
}

func TestReplaceEndsMailboxes(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	first := registered(t, s)
	l := testLink(t)
	pool := []string{ids.New()}
	if err := s.CreateLink(ctx, first.ID, l, links.MaxActive); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMailboxes(ctx, first.ID, pool, 10); err != nil {
		t.Fatal(err)
	}
	old, _ := s.ByTelegram(ctx, first.TelegramHash)
	if err := s.Replace(ctx, old, newAccount(t, first.TelegramHash), now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{pool[0], l.Mailbox} {
		if _, err := s.Mailbox(ctx, id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("mailbox %s of a replaced account: %v", id, err)
		}
	}
}
