package notices

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/botapi"
)

var now = time.Unix(1_800_000_000, 0)

func TestSealer(t *testing.T) {
	s, err := NewSealer(bytes.Repeat([]byte("k"), 32))
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Seal(123456789)
	b, _ := s.Seal(123456789)
	if bytes.Equal(a, b) {
		t.Fatal("sealing is deterministic: equal chats would show")
	}
	if chat, err := s.Open(a); err != nil || chat != 123456789 {
		t.Fatalf("Open = %d, %v", chat, err)
	}
	other, _ := NewSealer(bytes.Repeat([]byte("j"), 32))
	if _, err := other.Open(a); err == nil {
		t.Fatal("another key opened it")
	}
	a[len(a)-1] ^= 1
	if _, err := s.Open(a); err == nil {
		t.Fatal("a tampered seal opened")
	}
	if _, err := NewSealer([]byte("short")); err == nil {
		t.Fatal("accepted a short secret")
	}
}

type memDirectory struct {
	target directory.NoticeTarget
	found  bool
	sent   []int64
	off    bool
}

func (m *memDirectory) Notices(context.Context, string) (directory.NoticeTarget, error) {
	if !m.found {
		return directory.NoticeTarget{}, directory.ErrNotFound
	}
	t := m.target
	t.Off = m.off
	if len(m.sent) > 0 {
		t.LastMessage = m.sent[len(m.sent)-1]
	}
	return t, nil
}
func (m *memDirectory) NoticeSent(_ context.Context, _ string, id int64) error {
	m.sent = append(m.sent, id)
	return nil
}
func (m *memDirectory) NoticesOff(context.Context, string) error { m.off = true; return nil }

type memQuiet struct {
	mu    sync.Mutex
	until map[string]time.Time
	now   *time.Time
}

func (q *memQuiet) Start(_ context.Context, key string, until time.Time) (bool, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if u, ok := q.until[key]; ok && q.now.Before(u) {
		return false, nil
	}
	q.until[key] = until
	return true, nil
}

func TestNotify(t *testing.T) {
	var calls []string
	var bodies []map[string]any
	forbid := false
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		calls = append(calls, method)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		if forbid {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":403}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":` + map[bool]string{true: "10", false: "11"}[len(calls) == 1] + `}}`))
	}))
	defer telegram.Close()

	sealer, _ := NewSealer(bytes.Repeat([]byte("k"), 32))
	sealed, _ := sealer.Seal(4242)
	clock := now
	dir := &memDirectory{target: directory.NoticeTarget{SealedChat: sealed, Language: "fa"}, found: true}
	n := &Notifier{
		Directory: dir,
		Quiet:     &memQuiet{until: map[string]time.Time{}, now: &clock},
		Sealer:    sealer,
		Bot:       botapi.Client{Token: "1:T", API: telegram.URL},
		AppURL:    "https://example.github.io/",
		Now:       func() time.Time { return clock },
	}
	ctx := context.Background()

	if err := n.Notify(ctx, "acct"); err != nil {
		t.Fatal(err)
	}
	if calls[0] != "sendMessage" || bodies[0]["chat_id"] != float64(4242) || bodies[0]["text"] != "پیام جدید" {
		t.Fatalf("first notice: %v %v", calls, bodies[0])
	}
	// Within the quiet period: nothing.
	if err := n.Notify(ctx, "acct"); err != nil || len(calls) != 1 {
		t.Fatalf("second notice inside the interval: %v", calls)
	}
	// After it: the old notice is deleted, a new one sent.
	clock = now.Add(Interval + time.Second)
	if err := n.Notify(ctx, "acct"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, " ") != "sendMessage deleteMessage sendMessage" || bodies[1]["message_id"] != float64(10) {
		t.Fatalf("calls = %v, delete = %v", calls, bodies[1])
	}
	// Telegram refusing turns notices off; then none are tried.
	forbid = true
	clock = clock.Add(Interval + time.Second)
	if err := n.Notify(ctx, "acct"); err != nil || !dir.off {
		t.Fatalf("forbidden: %v, off %v", err, dir.off)
	}
	before := len(calls)
	clock = clock.Add(Interval + time.Second)
	_ = n.Notify(ctx, "acct")
	if len(calls) != before {
		t.Fatal("a notice was tried with notices off")
	}
	// A chat ID sealed under another key turns notices off.
	stale, _ := NewSealer(bytes.Repeat([]byte("o"), 32))
	old, _ := stale.Seal(4242)
	staleDir := &memDirectory{target: directory.NoticeTarget{SealedChat: old}, found: true}
	n2 := *n
	n2.Directory = staleDir
	n2.Quiet = &memQuiet{until: map[string]time.Time{}, now: &clock}
	if err := n2.Notify(ctx, "acct"); err != nil || !staleDir.off || len(calls) != before {
		t.Fatalf("stale seal: %v, off %v", err, staleDir.off)
	}
	// No notice setup at all: nothing, no error.
	if err := (&Notifier{Directory: &memDirectory{}, Now: n.Now}).Notify(ctx, "other"); err != nil {
		t.Fatal(err)
	}
}

func TestLanguage(t *testing.T) {
	for code, want := range map[string]string{"fa": "fa", "fa-IR": "fa", "en": "en", "de": "en", "": "en"} {
		if Language(code) != want {
			t.Errorf("Language(%q) = %q", code, Language(code))
		}
	}
}
