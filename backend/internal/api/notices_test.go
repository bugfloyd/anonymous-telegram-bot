package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/initdata/initdatatest"
)

type memNotices struct {
	mu       sync.Mutex
	notified []string
	fail     error
}

func (m *memNotices) Seal(_ context.Context, chat int64) ([]byte, error) {
	if m.fail != nil {
		return nil, m.fail
	}
	return []byte("sealed:" + strconv.FormatInt(chat, 10)), nil
}

func (m *memNotices) Notify(_ context.Context, account string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notified = append(m.notified, account)
	return m.fail
}

// launchIn signs launch data with a language, as Telegram does.
func launchIn(user int64, startParam, language string, h *harness) string {
	fields := map[string]string{
		"auth_date": strconv.FormatInt(h.now.Unix(), 10),
		"user":      `{"id":` + strconv.FormatInt(user, 10) + `,"first_name":"A","language_code":"` + language + `","allows_write_to_pm":true}`,
	}
	if startParam != "" {
		fields["start_param"] = startParam
	}
	return initdatatest.Sign(botID, fields)
}

func TestSetNotices(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	n := &memNotices{}
	h.server.Notices = n
	m := h.account(t, userID)

	if _, out := h.as(t, m, http.MethodGet, "/account", nil, false); out["notices"] != false {
		t.Fatalf("before: %v", out)
	}
	status, out := h.as(t, m, http.MethodPost, "/notices", map[string]any{"initData": launchIn(userID, "", "fa-IR", h)}, false)
	if status != http.StatusOK {
		t.Fatalf("notices: %d %v", status, out)
	}
	a := h.dir.accounts[h.dir.telegram[directory.TelegramHash(h.server.Pepper, userID)]]
	if string(a.chat) != "sealed:"+strconv.FormatInt(userID, 10) || a.language != "fa" {
		t.Fatalf("stored %q %q", a.chat, a.language)
	}
	if _, out := h.as(t, m, http.MethodGet, "/account", nil, false); out["notices"] != true {
		t.Fatalf("after: %v", out)
	}
	// A registration launch works too.
	if status, out := h.as(t, m, http.MethodPost, "/notices", map[string]any{"initData": launchIn(userID, "reg"+"abc", "en", h)}, false); status != http.StatusOK {
		t.Fatalf("registration launch: %d %v", status, out)
	}
}

func TestSetNoticesRejects(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	n := &memNotices{}
	h.server.Notices = n
	m := h.account(t, userID)
	try := func(initData string) (int, map[string]any) {
		return h.as(t, m, http.MethodPost, "/notices", map[string]any{"initData": initData}, false)
	}

	// Launches that name a link or an invitation, or another user, or nothing.
	for _, bad := range []string{
		launchIn(userID, "1"+ids.New()[:11]+"x", "en", h),
		launchIn(userID, "invABCDEFGH", "en", h),
		launchIn(userID+1, "", "en", h),
		"auth_date=1&user=%7B%22id%22%3A1%7D&hash=00",
	} {
		status, out := try(bad)
		wantError(t, status, out, http.StatusUnauthorized, "init_data_rejected")
	}
	status, out := try("")
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	// Without a login.
	status, out = h.call(t, nil, "", http.MethodPost, "/notices", map[string]any{"initData": launchIn(userID, "", "en", h)}, false)
	wantError(t, status, out, http.StatusUnauthorized, "session_expired")

	// Rate limited.
	for range NoticeSetupsPerDay {
		try(launchIn(userID, "", "en", h))
	}
	status, out = try(launchIn(userID, "", "en", h))
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")

	// Notices not configured on this server.
	h.server.Notices = nil
	status, out = try(launchIn(userID, "", "en", h))
	wantError(t, status, out, http.StatusNotFound, "not_found")
}

func TestSendNotifies(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	n := &memNotices{}
	h.server.Notices = n
	owner := h.account(t, userID)
	_, out := h.as(t, owner, http.MethodPost, "/mailboxes", map[string]any{"count": 1}, false)
	mailbox := out["mailboxes"].([]any)[0].(string)
	env := clientEnvelope(t)
	send := func(mb string, quiet bool) {
		t.Helper()
		if status, out := h.post(t, "/send", map[string]any{"mailbox": mb, "envelope": env, "token": mintToken(t, h.now), "quiet": quiet}); status != http.StatusOK {
			t.Fatalf("send: %d %v", status, out)
		}
	}

	send(mailbox, false)
	if len(n.notified) != 1 || !ids.Valid(n.notified[0]) {
		t.Fatalf("notified = %v", n.notified)
	}
	// Seen receipts are quiet.
	send(mailbox, true)
	if len(n.notified) != 1 {
		t.Fatalf("a quiet send notified: %v", n.notified)
	}
	// Unknown mailboxes make the same request, for nobody.
	send(ids.New(), false)
	if len(n.notified) != 2 || n.notified[1] != "" {
		t.Fatalf("unknown mailbox: %v", n.notified)
	}
	// A failing notice doesn't fail the send, and is logged without details.
	n.fail = errors.New("boom 7357135791")
	send(mailbox, false)
	if !strings.Contains(h.logs.String(), "notice request failed") || strings.Contains(h.logs.String(), "7357135791") {
		t.Fatalf("logs: %s", h.logs.String())
	}
}

type memPush struct {
	online map[string]bool
	pushed []string
}

func (m *memPush) Push(_ context.Context, account string) (bool, error) {
	m.pushed = append(m.pushed, account)
	return m.online[account], nil
}

func TestSendPushesToOpenApps(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	n := &memNotices{}
	p := &memPush{online: map[string]bool{}}
	h.server.Notices, h.server.Push = n, p
	owner := h.account(t, userID)
	_, out := h.as(t, owner, http.MethodPost, "/mailboxes", map[string]any{"count": 1}, false)
	mailbox := out["mailboxes"].([]any)[0].(string)
	env := clientEnvelope(t)
	send := func(mb string, quiet bool) {
		t.Helper()
		if status, out := h.post(t, "/send", map[string]any{"mailbox": mb, "envelope": env, "token": mintToken(t, h.now), "quiet": quiet}); status != http.StatusOK {
			t.Fatalf("send: %d %v", status, out)
		}
	}

	// The app is closed: pushed to nobody, so a notice.
	send(mailbox, false)
	account := p.pushed[0]
	if !ids.Valid(account) || len(n.notified) != 1 || n.notified[0] != account {
		t.Fatalf("closed app: pushed %v, notified %v", p.pushed, n.notified)
	}
	// Open: the push is enough; the notice request is the no-op one.
	p.online[account] = true
	send(mailbox, false)
	if p.pushed[1] != account || n.notified[1] != "" {
		t.Fatalf("open app: pushed %v, notified %v", p.pushed, n.notified)
	}
	// Receipts are pushed, never noticed.
	send(mailbox, true)
	if p.pushed[2] != account || len(n.notified) != 2 {
		t.Fatalf("receipt: pushed %v, notified %v", p.pushed, n.notified)
	}
	// Unknown mailboxes make the same requests, for nobody.
	send(ids.New(), false)
	if p.pushed[3] != "" || n.notified[2] != "" {
		t.Fatalf("unknown mailbox: pushed %v, notified %v", p.pushed, n.notified)
	}
}
