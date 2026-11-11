package api

import (
	"net/http"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/invitations"
)

// member is a logged-in test account.
type member struct {
	d     *device
	token string
}

func (h *harness) account(t *testing.T, user int64) member {
	t.Helper()
	d := newDevice(t)
	d.user = user
	return member{d: d, token: h.loggedIn(t, d, false)}
}

func (h *harness) as(t *testing.T, m member, method, path string, body any, sign bool) (int, map[string]any) {
	t.Helper()
	return h.call(t, m.d, m.token, method, path, body, sign)
}

func TestInvitationChain(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = true
	h.makeAdmin(userID)
	admin := h.account(t, userID)
	alice := h.account(t, userID+1)
	bob := h.account(t, userID+2)

	status, out := h.as(t, admin, http.MethodGet, "/invitations", nil, false)
	if status != http.StatusOK || out["unlimited"] != true || out["admin"] != true || out["maxUses"] != float64(invitations.AdminMaxUses) {
		t.Fatalf("admin invitations: %d %v", status, out)
	}
	status, out = h.as(t, admin, http.MethodPost, "/invitations", map[string]any{"uses": 2}, true)
	if status != http.StatusCreated {
		t.Fatalf("admin code: %d %v", status, out)
	}
	code := out["code"].(string)

	// Alice joins with the pasted invitation link: level 1, may now own links.
	link := "https://t.me/some_bot/app?startapp=" + invitations.Payload(code)
	if status, out := h.as(t, alice, http.MethodPost, "/invitations/redeem", map[string]any{"code": link}, false); status != http.StatusOK || out["level"] != float64(1) {
		t.Fatalf("redeem: %d %v", status, out)
	}
	if _, out := h.as(t, alice, http.MethodGet, "/account", nil, false); out["canCreateLinks"] != true {
		t.Fatalf("alice account: %v", out)
	}
	status, out = h.as(t, alice, http.MethodPost, "/invitations/redeem", map[string]any{"code": code}, false)
	wantError(t, status, out, http.StatusConflict, "already_member")
	status, out = h.as(t, bob, http.MethodPost, "/invitations/redeem", map[string]any{"code": "zzzz-zzzz"}, false)
	wantError(t, status, out, http.StatusNotFound, "invalid_invitation")
	status, out = h.as(t, bob, http.MethodPost, "/invitations/redeem", map[string]any{"code": "not a code"}, false)
	wantError(t, status, out, http.StatusNotFound, "invalid_invitation")

	// The admin sees how much of the code is used.
	_, out = h.as(t, admin, http.MethodGet, "/invitations", nil, false)
	codes := out["codes"].([]any)
	if len(codes) != 1 || codes[0].(map[string]any)["left"] != float64(1) {
		t.Fatalf("admin codes: %v", codes)
	}

	// Members spend their level's allowance; admins set it.
	status, out = h.as(t, alice, http.MethodPost, "/invitations", map[string]any{"uses": 1}, true)
	wantError(t, status, out, http.StatusConflict, "no_invitations_left")
	if status, out := h.as(t, admin, http.MethodPut, "/admin/levels", map[string]any{"level": 1, "invitations": 2}, true); status != http.StatusOK {
		t.Fatalf("set level: %d %v", status, out)
	}
	if _, out := h.as(t, alice, http.MethodGet, "/invitations", nil, false); out["remaining"] != float64(2) || out["level"] != float64(1) {
		t.Fatalf("alice invitations: %v", out)
	}
	status, out = h.as(t, alice, http.MethodPost, "/invitations", map[string]any{"uses": 3}, true)
	wantError(t, status, out, http.StatusConflict, "no_invitations_left")
	status, out = h.as(t, alice, http.MethodPost, "/invitations", map[string]any{"uses": 2}, true)
	if status != http.StatusCreated {
		t.Fatalf("alice code: %d %v", status, out)
	}
	if status, out := h.as(t, bob, http.MethodPost, "/invitations/redeem", map[string]any{"code": out["code"]}, false); out["level"] != float64(2) {
		t.Fatalf("bob redeem: %d %v", status, out)
	}

	// Admins grant extra invitations to a member through one of their links.
	aliceLink := newLinkKeys(t)
	if status, out := h.as(t, alice, http.MethodPost, "/links", aliceLink.create(t, h.now), true); status != http.StatusCreated {
		t.Fatalf("alice link: %d %v", status, out)
	}
	if status, out := h.as(t, admin, http.MethodPost, "/admin/grant", map[string]any{"link": aliceLink.id, "invitations": 3}, true); status != http.StatusOK {
		t.Fatalf("grant: %d %v", status, out)
	}
	if _, out := h.as(t, alice, http.MethodGet, "/invitations", nil, false); out["remaining"] != float64(3) {
		t.Fatalf("after grant: %v", out)
	}
	status, out = h.as(t, admin, http.MethodPost, "/admin/grant", map[string]any{"link": newLinkKeys(t).id, "invitations": 1}, true)
	wantError(t, status, out, http.StatusNotFound, "not_found")
	if _, out := h.as(t, admin, http.MethodGet, "/admin/levels", nil, false); len(out["levels"].([]any)) != 1 {
		t.Fatalf("levels: %v", out)
	}
}

func TestInvitationRules(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = true
	stranger := h.account(t, userID)

	status, out := h.as(t, stranger, http.MethodPost, "/invitations", map[string]any{"uses": 1}, true)
	wantError(t, status, out, http.StatusForbidden, "invitation_required")
	status, out = h.as(t, stranger, http.MethodGet, "/admin/levels", nil, false)
	wantError(t, status, out, http.StatusForbidden, "admin_only")
	status, out = h.as(t, stranger, http.MethodPut, "/admin/levels", map[string]any{"level": 1, "invitations": 9}, true)
	wantError(t, status, out, http.StatusForbidden, "admin_only")
	status, out = h.as(t, stranger, http.MethodPost, "/admin/grant", map[string]any{"link": newLinkKeys(t).id, "invitations": 9}, true)
	wantError(t, status, out, http.StatusForbidden, "admin_only")
	// Admin actions need a device signature, like other sensitive ones.
	h.makeAdmin(userID)
	status, out = h.as(t, stranger, http.MethodPut, "/admin/levels", map[string]any{"level": 1, "invitations": 9}, false)
	wantError(t, status, out, http.StatusUnauthorized, "signature_rejected")
	status, out = h.as(t, stranger, http.MethodPut, "/admin/levels", map[string]any{"level": 0, "invitations": 9}, true)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	status, out = h.as(t, stranger, http.MethodPost, "/invitations", map[string]any{"uses": invitations.AdminMaxUses + 1}, true)
	wantError(t, status, out, http.StatusBadRequest, "bad_request")

	// Granting to someone who isn't a member.
	other := h.account(t, userID+1)
	h.server.InviteOnly = false
	l := newLinkKeys(t)
	if status, out := h.as(t, other, http.MethodPost, "/links", l.create(t, h.now), true); status != http.StatusCreated {
		t.Fatalf("link: %d %v", status, out)
	}
	status, out = h.as(t, stranger, http.MethodPost, "/admin/grant", map[string]any{"link": l.id, "invitations": 1}, true)
	wantError(t, status, out, http.StatusConflict, "not_member")
}

func TestRedeemRateLimit(t *testing.T) {
	h := newHarness(t)
	m := h.account(t, userID)
	for range invitations.RedeemsPerDay {
		status, out := h.as(t, m, http.MethodPost, "/invitations/redeem", map[string]any{"code": "zzzz-zzzz"}, false)
		wantError(t, status, out, http.StatusNotFound, "invalid_invitation")
	}
	status, out := h.as(t, m, http.MethodPost, "/invitations/redeem", map[string]any{"code": "zzzz-zzzz"}, false)
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")
}
