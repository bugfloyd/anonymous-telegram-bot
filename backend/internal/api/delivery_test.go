package api

import (
	"encoding/base64"
	"encoding/hex"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/envelope"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

// clientEnvelope is an envelope sealed by the web client.
func clientEnvelope(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../envelope/testdata/client-envelopes.hex")
	if err != nil {
		t.Fatal(err)
	}
	env, err := hex.DecodeString(strings.Fields(string(raw))[0])
	if err != nil {
		t.Fatal(err)
	}
	return base64.RawURLEncoding.EncodeToString(env)
}

func TestDelivery(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	paused := 0
	h.server.Pause = func() { paused++ }
	owner := h.account(t, userID)
	sender := h.account(t, userID+1)
	env := clientEnvelope(t)

	// The owner keeps a pool of mailboxes.
	status, out := h.as(t, owner, http.MethodPost, "/mailboxes", map[string]any{"count": 3}, false)
	if status != http.StatusCreated {
		t.Fatalf("mailboxes: %d %v", status, out)
	}
	pool := out["mailboxes"].([]any)
	if len(pool) != 3 {
		t.Fatalf("pool = %v", pool)
	}
	mailbox := pool[0].(string)

	// Anyone can send, without logging in.
	status, out = h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now)})
	if status != http.StatusOK || len(out) != 0 {
		t.Fatalf("send: %d %v", status, out)
	}
	// Unknown mailboxes get the same answer, after the same pause, and nothing is stored.
	unknownStatus, unknownOut := h.post(t, "/send", map[string]any{"mailbox": ids.New(), "envelope": env, "token": mintToken(t, h.now)})
	if unknownStatus != status || len(unknownOut) != 0 || paused != 1 {
		t.Fatalf("unknown mailbox: %d %v, paused %d", unknownStatus, unknownOut, paused)
	}

	// The owner's device fetches and confirms.
	status, out = h.as(t, owner, http.MethodGet, "/inbox", nil, false)
	items := out["items"].([]any)
	if status != http.StatusOK || len(items) != 1 || out["more"] != false {
		t.Fatalf("inbox: %d %v", status, out)
	}
	item := items[0].(map[string]any)
	if item["mailbox"] != mailbox || item["envelope"] != env {
		t.Fatalf("item = %v", item)
	}
	if status, out := h.as(t, owner, http.MethodPost, "/ack", map[string]any{"ids": []string{item["id"].(string)}}, false); status != http.StatusOK {
		t.Fatalf("ack: %d %v", status, out)
	}
	if _, out := h.as(t, owner, http.MethodGet, "/inbox", nil, false); len(out["items"].([]any)) != 0 {
		t.Fatalf("inbox after ack: %v", out)
	}
	// Nothing reached the sender's own inbox.
	if _, out := h.as(t, sender, http.MethodGet, "/inbox", nil, false); len(out["items"].([]any)) != 0 {
		t.Fatalf("sender inbox: %v", out)
	}

	// Discarding (a block): only the owner can, and sends then vanish silently.
	status, out = h.as(t, sender, http.MethodPost, "/mailboxes/"+mailbox+"/discard", nil, false)
	wantError(t, status, out, http.StatusNotFound, "not_found")
	if status, out := h.as(t, owner, http.MethodPost, "/mailboxes/"+mailbox+"/discard", nil, false); status != http.StatusOK {
		t.Fatalf("discard: %d %v", status, out)
	}
	if status, _ := h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now)}); status != http.StatusOK {
		t.Fatalf("send to a discarded mailbox: %d", status)
	}
	if _, out := h.as(t, owner, http.MethodGet, "/inbox", nil, false); len(out["items"].([]any)) != 0 {
		t.Fatalf("a discarded mailbox delivered: %v", out)
	}
}

func TestSendThroughALink(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	owner := h.account(t, userID)
	l := newLinkKeys(t)
	_, out := h.as(t, owner, http.MethodPost, "/links", l.create(t, h.now), true)
	intro := out["mailbox"].(string)
	if _, bundle := h.call(t, nil, "", http.MethodGet, "/links/"+l.id, nil, false); bundle["mailbox"] != intro {
		t.Fatalf("bundle mailbox = %v, want %s", bundle["mailbox"], intro)
	}
	if status, _ := h.post(t, "/send", map[string]any{"mailbox": intro, "envelope": clientEnvelope(t), "token": mintToken(t, h.now)}); status != http.StatusOK {
		t.Fatalf("send: %d", status)
	}
	if _, out := h.as(t, owner, http.MethodGet, "/inbox", nil, false); len(out["items"].([]any)) != 1 {
		t.Fatalf("inbox: %v", out)
	}
	// Intro mailboxes go with their link, not one by one.
	status, res := h.as(t, owner, http.MethodPost, "/mailboxes/"+intro+"/discard", nil, false)
	wantError(t, status, res, http.StatusNotFound, "not_found")
}

func TestSendRejects(t *testing.T) {
	h := newHarness(t)
	env := clientEnvelope(t)
	raw, _ := base64.RawURLEncoding.DecodeString(env)

	status, out := h.post(t, "/send", map[string]any{"mailbox": "x", "envelope": env})
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	status, out = h.post(t, "/send", map[string]any{"mailbox": ids.New(), "envelope": base64.RawURLEncoding.EncodeToString(raw[:len(raw)-1])})
	wantError(t, status, out, http.StatusBadRequest, "invalid_envelope")
	status, out = h.post(t, "/send", map[string]any{"mailbox": ids.New(), "envelope": "not base64!"})
	wantError(t, status, out, http.StatusBadRequest, "invalid_envelope")
	big := make([]byte, envelope.MaxLen+1)
	big[0], big[1] = envelope.Suite, 1
	status, out = h.post(t, "/send", map[string]any{"mailbox": ids.New(), "envelope": base64.RawURLEncoding.EncodeToString(big)})
	if status != http.StatusBadRequest {
		t.Fatalf("oversized envelope: %d %v", status, out)
	}

	// Per-mailbox cap.
	mailbox := ids.New()
	for range SendsPerMailboxPerMinute {
		h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now)})
	}
	status, out = h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now)})
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")
}

func TestMailboxLimits(t *testing.T) {
	h := newHarness(t)
	m := h.account(t, userID)
	status, out := h.as(t, m, http.MethodPost, "/mailboxes", map[string]any{"count": MailboxBatch + 1}, false)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	status, out = h.as(t, m, http.MethodPost, "/mailboxes", map[string]any{"count": 0}, false)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	for range MailboxesPerDay {
		h.as(t, m, http.MethodPost, "/mailboxes", map[string]any{"count": 1}, false)
	}
	status, out = h.as(t, m, http.MethodPost, "/mailboxes", map[string]any{"count": 1}, false)
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")

	status, out = h.as(t, m, http.MethodPost, "/ack", map[string]any{"ids": []string{}}, false)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	status, out = h.as(t, m, http.MethodPost, "/ack", map[string]any{"ids": []string{"x"}}, false)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
}

func TestCheckMailboxes(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	owner := h.account(t, userID)
	other := h.account(t, userID+1)
	_, out := h.as(t, owner, http.MethodPost, "/mailboxes", map[string]any{"count": 2}, false)
	mine := out["mailboxes"].([]any)
	_, out = h.as(t, other, http.MethodPost, "/mailboxes", map[string]any{"count": 1}, false)
	theirs := out["mailboxes"].([]any)[0].(string)
	gone := ids.New()

	status, out := h.as(t, owner, http.MethodPost, "/mailboxes/check", map[string]any{"ids": []any{mine[0], mine[1], theirs, gone}}, false)
	missing := out["missing"].([]any)
	if status != http.StatusOK || len(missing) != 2 || missing[0] != theirs || missing[1] != gone {
		t.Fatalf("check: %d %v", status, out)
	}
	// After a discard, it's missing too.
	h.as(t, owner, http.MethodPost, "/mailboxes/"+mine[0].(string)+"/discard", nil, false)
	if _, out := h.as(t, owner, http.MethodPost, "/mailboxes/check", map[string]any{"ids": []any{mine[0], mine[1]}}, false); len(out["missing"].([]any)) != 1 {
		t.Fatalf("after discard: %v", out)
	}
	for _, bad := range []any{[]any{}, []any{"x"}} {
		status, out := h.as(t, owner, http.MethodPost, "/mailboxes/check", map[string]any{"ids": bad}, false)
		wantError(t, status, out, http.StatusBadRequest, "bad_request")
	}
	status, out = h.call(t, nil, "", http.MethodPost, "/mailboxes/check", map[string]any{"ids": []any{gone}}, false)
	wantError(t, status, out, http.StatusUnauthorized, "session_expired")
}
