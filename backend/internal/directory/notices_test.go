package directory

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

func TestNotices(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	a := registered(t, s)

	if _, err := s.Notices(ctx, a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("before setup: %v", err)
	}
	if err := s.SetNotices(ctx, a.ID, []byte("sealed"), "fa"); err != nil {
		t.Fatal(err)
	}
	if err := s.NoticeSent(ctx, a.ID, 77); err != nil {
		t.Fatal(err)
	}
	on := func() bool {
		t.Helper()
		d, err := s.Device(ctx, a.ID, a.DeviceID, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		return d.Notices
	}
	if !on() {
		t.Fatal("device read says notices are off")
	}
	n, err := s.Notices(ctx, a.ID)
	if err != nil || !bytes.Equal(n.SealedChat, []byte("sealed")) || n.Language != "fa" || n.LastMessage != 77 || n.Off {
		t.Fatalf("Notices = %+v, %v", n, err)
	}
	if err := s.NoticesOff(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Notices(ctx, a.ID); !n.Off || on() {
		t.Fatal("notices still on")
	}
	// Setting up again turns them back on.
	if err := s.SetNotices(ctx, a.ID, []byte("sealed2"), "en"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Notices(ctx, a.ID); n.Off || n.Language != "en" || !on() {
		t.Fatalf("after re-enabling: %+v", n)
	}
	if err := s.SetNotices(ctx, ids.New(), []byte("x"), "en"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown account: %v", err)
	}
}
