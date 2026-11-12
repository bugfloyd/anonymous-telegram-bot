package api

import (
	"net/http"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

func TestLookupAccount(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	lookup := func(initData string) (int, map[string]any) {
		return h.post(t, "/accounts/lookup", map[string]any{"initData": initData})
	}

	// No account yet.
	status, out := lookup(launchIn(userID, "", "en", h))
	if status != http.StatusOK || out["exists"] != false {
		t.Fatalf("before: %d %v", status, out)
	}
	h.account(t, userID)
	// Set up (on another device): it exists. A registration launch may ask too.
	for _, start := range []string{"", "regabc"} {
		if status, out := lookup(launchIn(userID, start, "en", h)); status != http.StatusOK || out["exists"] != true {
			t.Fatalf("after (%q): %d %v", start, status, out)
		}
	}
	// Someone else: no.
	if _, out := lookup(launchIn(userID+1, "", "en", h)); out["exists"] != false {
		t.Fatalf("another user: %v", out)
	}

	// Launches that name a link or an invitation are refused, and bad data.
	for _, bad := range []string{launchIn(userID, "1"+ids.New()[:11]+"x", "en", h), launchIn(userID, "invABCDEFGH", "en", h), "auth_date=1&hash=00"} {
		status, out := lookup(bad)
		wantError(t, status, out, http.StatusUnauthorized, "init_data_rejected")
	}
	status, out = lookup("")
	wantError(t, status, out, http.StatusBadRequest, "bad_request")

	// Rate limited per Telegram user.
	for range LookupsPerDay {
		lookup(launchIn(userID+2, "", "en", h))
	}
	status, out = lookup(launchIn(userID+2, "", "en", h))
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")
}
