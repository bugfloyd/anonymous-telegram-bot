package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/tokens"
)

func TestSendNeedsAFreshToken(t *testing.T) {
	h := newHarness(t)
	env := clientEnvelope(t)
	mailbox := ids.New()

	status, out := h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token")
	status, out = h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": b64(randomBytes(t, 354))})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token")

	token := mintToken(t, h.now)
	if status, out := h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": token}); status != http.StatusOK {
		t.Fatalf("send: %d %v", status, out)
	}
	status, out = h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": token})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token") // spent

	// Last week's tokens still work; older ones don't.
	if status, _ := h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now.Add(-tokens.Epoch))}); status != http.StatusOK {
		t.Fatalf("last week's token: %d", status)
	}
	status, out = h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now.Add(-2*tokens.Epoch))})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token")
}

func TestRateLimitedSendKeepsItsToken(t *testing.T) {
	h := newHarness(t)
	env := clientEnvelope(t)
	mailbox := ids.New()
	for range SendsPerMailboxPerMinute {
		h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now)})
	}
	token := mintToken(t, h.now)
	status, out := h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": token})
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")
	// Another mailbox, a minute later: the same token is still good.
	h.now = h.now.Add(time.Minute)
	if status, out := h.post(t, "/send", map[string]any{"mailbox": ids.New(), "envelope": env, "token": token}); status != http.StatusOK {
		t.Fatalf("token after a rate-limited send: %d %v", status, out)
	}
}

// A spent token replayed at a mailbox must not use up the mailbox's limit
// for everyone else.
func TestReplayedTokenDoesNotCountAgainstTheMailbox(t *testing.T) {
	h := newHarness(t)
	env := clientEnvelope(t)
	mailbox := ids.New()
	spent := mintToken(t, h.now)
	if status, out := h.post(t, "/send", map[string]any{"mailbox": ids.New(), "envelope": env, "token": spent}); status != http.StatusOK {
		t.Fatalf("send: %d %v", status, out)
	}
	for range 2 * SendsPerMailboxPerMinute {
		status, out := h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": spent})
		wantError(t, status, out, http.StatusUnauthorized, "invalid_token")
	}
	if status, out := h.post(t, "/send", map[string]any{"mailbox": mailbox, "envelope": env, "token": mintToken(t, h.now)}); status != http.StatusOK {
		t.Fatalf("send after replays: %d %v", status, out)
	}
}

func TestIssueTokens(t *testing.T) {
	h := newHarness(t)
	m := h.account(t, userID)
	blinded := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			// Any value below the modulus can be signed; a real client blinds a token input.
			b := randomBytes(t, testIssuer().BlindedLen())
			b[0] = 0
			out[i] = b64(b)
		}
		return out
	}
	status, out := h.as(t, m, http.MethodPost, "/tokens", map[string]any{"blinded": blinded(3)}, false)
	if status != http.StatusOK || len(out["signatures"].([]any)) != 3 {
		t.Fatalf("tokens: %d %v", status, out)
	}

	status, out = h.as(t, m, http.MethodPost, "/tokens", map[string]any{"blinded": blinded(TokenBatch + 1)}, false)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	status, out = h.as(t, m, http.MethodPost, "/tokens", map[string]any{"blinded": []string{b64(randomBytes(t, 10))}}, false)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")

	// The daily allowance: 3 used, so 197 more, then nothing.
	for range (TokensPerDay - 3) / TokenBatch {
		h.as(t, m, http.MethodPost, "/tokens", map[string]any{"blinded": blinded(TokenBatch)}, false)
	}
	rest := (TokensPerDay - 3) % TokenBatch
	if status, _ := h.as(t, m, http.MethodPost, "/tokens", map[string]any{"blinded": blinded(rest)}, false); status != http.StatusOK {
		t.Fatalf("last %d of the day: %d", rest, status)
	}
	status, out = h.as(t, m, http.MethodPost, "/tokens", map[string]any{"blinded": blinded(1)}, false)
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")

	// Needs a login.
	status, out = h.call(t, nil, "", http.MethodPost, "/tokens", map[string]any{"blinded": blinded(1)}, false)
	wantError(t, status, out, http.StatusUnauthorized, "session_expired")
}
