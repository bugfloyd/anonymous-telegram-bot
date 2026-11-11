package api

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/links"
	"github.com/incognitochat/incognitochat/backend/internal/session"
)

func TestSignedRequestsRejects(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	d := newDevice(t)
	token := h.loggedIn(t, d, false)
	body := newLinkKeys(t).create(t, h.now)

	// No token, or an expired one.
	status, out := h.call(t, d, "", http.MethodPost, "/links", body, true)
	wantError(t, status, out, http.StatusUnauthorized, "session_expired")
	h.now = h.now.Add(session.TTL + time.Second)
	status, out = h.call(t, d, token, http.MethodGet, "/account", nil, false)
	wantError(t, status, out, http.StatusUnauthorized, "session_expired")
	h.now = start
	token = h.token(t, d)

	// A token without a signature.
	status, out = h.call(t, d, token, http.MethodPost, "/links", body, false)
	wantError(t, status, out, http.StatusUnauthorized, "signature_rejected")

	// A signature over another body.
	raw, _ := json.Marshal(body)
	var req httpapi.Request
	req.RawPath = "/links"
	req.RequestContext.HTTP.Method = http.MethodPost
	signed := session.SignedRequest{AccountID: d.account, DeviceID: d.id, Method: http.MethodPost, Path: "/links",
		Body: []byte("{}"), Timestamp: h.now.Unix(), Nonce: b64(randomBytes(t, 16))}
	sig := signP1363(t, d.dsk, devicesig.Message(devicesig.LabelRequest, session.RequestParts(signed)...))
	req.Headers = map[string]string{
		"content-type":       "application/json",
		"authorization":      "Bearer " + token,
		"x-device-signature": strconv.FormatInt(signed.Timestamp, 10) + "." + signed.Nonce + "." + b64(sig),
	}
	req.Body = string(raw)
	resp, _ := h.router.Serve(context.Background(), req)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("signature over another body: %d %s", resp.StatusCode, resp.Body)
	}
	// The refused attempt didn't use up the nonce: with the signed body the
	// request gets past authentication (and fails as a link)...
	req.Body = "{}"
	resp, _ = h.router.Serve(context.Background(), req)
	if resp.StatusCode != http.StatusBadRequest || resp.Body != `{"error":"invalid_link"}` {
		t.Fatalf("signed body: %d %s", resp.StatusCode, resp.Body)
	}
	// ...and sending it again is a replay.
	resp, _ = h.router.Serve(context.Background(), req)
	if resp.StatusCode != http.StatusUnauthorized || resp.Body != `{"error":"signature_rejected"}` {
		t.Fatalf("replayed signature: %d %s", resp.StatusCode, resp.Body)
	}
}

func TestReplacedAccountIsCutOffAtOnce(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	old := newDevice(t)
	oldToken := h.loggedIn(t, old, false)
	l := newLinkKeys(t)
	if status, out := h.call(t, old, oldToken, http.MethodPost, "/links", l.create(t, h.now), true); status != http.StatusCreated {
		t.Fatalf("create: %d %v", status, out)
	}

	h.loggedIn(t, newDevice(t), true)

	// The old token hasn't expired, but the account is gone.
	status, out := h.call(t, old, oldToken, http.MethodGet, "/account", nil, false)
	wantError(t, status, out, http.StatusGone, "account_replaced")
	status, out = h.call(t, old, oldToken, http.MethodPost, "/links", newLinkKeys(t).create(t, h.now), true)
	wantError(t, status, out, http.StatusGone, "account_replaced")
	status, out = h.call(t, nil, "", http.MethodGet, "/links/"+l.id, nil, false)
	wantError(t, status, out, http.StatusNotFound, "not_found")
}

// call sends a request as a logged-in device; sign adds a device signature.
func (h *harness) call(t *testing.T, d *device, token, method, path string, body any, sign bool) (int, map[string]any) {
	t.Helper()
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			t.Fatal(err)
		}
	}
	var req httpapi.Request
	req.RawPath = path
	req.RequestContext.HTTP.Method = method
	req.Headers = map[string]string{"content-type": "application/json"}
	if token != "" {
		req.Headers["authorization"] = "Bearer " + token
	}
	req.Body = string(raw)
	if sign {
		signed := session.SignedRequest{
			AccountID: d.account, DeviceID: d.id, Method: method, Path: path, Body: raw,
			Timestamp: h.now.Unix(), Nonce: b64(randomBytes(t, 16)),
		}
		sig := signP1363(t, d.dsk, devicesig.Message(devicesig.LabelRequest, session.RequestParts(signed)...))
		req.Headers["x-device-signature"] = strconv.FormatInt(signed.Timestamp, 10) + "." + signed.Nonce + "." + b64(sig)
	}
	resp, err := h.router.Serve(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resp.Body), &out); err != nil {
		t.Fatalf("response body %q: %v", resp.Body, err)
	}
	return resp.StatusCode, out
}

// loggedIn registers a device and returns a session token for it.
func (h *harness) loggedIn(t *testing.T, d *device, replace bool) string {
	t.Helper()
	if status, out := h.register(t, d, replace); status != http.StatusOK {
		t.Fatalf("register: %d %v", status, out)
	}
	return h.token(t, d)
}

// token logs a registered device in.
func (h *harness) token(t *testing.T, d *device) string {
	t.Helper()
	status, out := h.post(t, "/session", d.login(t, h.now))
	if status != http.StatusOK {
		t.Fatalf("login: %d %v", status, out)
	}
	return out["token"].(string)
}

type linkKeys struct {
	id  string
	lsk *ecdsa.PrivateKey
}

func newLinkKeys(t *testing.T) linkKeys {
	lsk, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	return linkKeys{id: b64(randomBytes(t, 8)), lsk: lsk}
}

// encryptionKey returns a signed encryption key valid from the hour of at for four weeks.
func (l linkKeys) encryptionKey(t *testing.T, at time.Time) map[string]any {
	t.Helper()
	ek, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	k := links.Key{Key: ek.PublicKey().Bytes(), From: at.Truncate(time.Hour), Until: at.Truncate(time.Hour).Add(28 * 24 * time.Hour)}
	sig := signP1363(t, l.lsk, devicesig.Message(devicesig.LabelLinkKey, links.KeyParts(k)...))
	return map[string]any{"key": b64(k.Key), "from": k.From.Unix(), "until": k.Until.Unix(), "signature": b64(sig)}
}

func (l linkKeys) create(t *testing.T, at time.Time) map[string]any {
	return map[string]any{"id": l.id, "signingKey": b64(raw(t, l.lsk)), "keys": []any{l.encryptionKey(t, at)}}
}

func (h *harness) makeAdmin(user int64) {
	if h.server.Admins == nil {
		h.server.Admins = map[string]bool{}
	}
	h.server.Admins[directory.TelegramHash(h.server.Pepper, user)] = true
}

func TestLinksNeedAnInvitation(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = true
	d := newDevice(t)
	token := h.loggedIn(t, d, false)

	status, out := h.call(t, d, token, http.MethodGet, "/account", nil, false)
	if status != http.StatusOK || out["canCreateLinks"] != false || out["admin"] != false {
		t.Fatalf("account: %d %v", status, out)
	}
	status, out = h.call(t, d, token, http.MethodPost, "/links", newLinkKeys(t).create(t, h.now), true)
	wantError(t, status, out, http.StatusForbidden, "invitation_required")

	h.server.InviteOnly = false
	if status, out := h.call(t, d, token, http.MethodPost, "/links", newLinkKeys(t).create(t, h.now), true); status != http.StatusCreated {
		t.Fatalf("open mode: %d %v", status, out)
	}
}

func TestLinkLifecycle(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = true
	h.makeAdmin(userID)
	d := newDevice(t)
	token := h.loggedIn(t, d, false)

	status, out := h.call(t, d, token, http.MethodGet, "/account", nil, false)
	if status != http.StatusOK || out["canCreateLinks"] != true || out["admin"] != true || out["maxLinks"] != float64(links.MaxActive) {
		t.Fatalf("account: %d %v", status, out)
	}

	l := newLinkKeys(t)
	create := l.create(t, h.now)
	if status, out := h.call(t, d, token, http.MethodPost, "/links", create, true); status != http.StatusCreated || out["id"] != l.id {
		t.Fatalf("create: %d %v", status, out)
	}
	status, out = h.call(t, d, token, http.MethodPost, "/links", create, true)
	wantError(t, status, out, http.StatusConflict, "conflict")

	// Anyone can read the bundle, which says nothing about the owner.
	status, out = h.call(t, nil, "", http.MethodGet, "/links/"+l.id, nil, false)
	if status != http.StatusOK || out["signingKey"] != b64(raw(t, l.lsk)) || len(out) != 3 || !ids.Valid(out["mailbox"].(string)) {
		t.Fatalf("bundle: %d %v", status, out)
	}
	if keys := out["keys"].([]any); len(keys) != 1 || keys[0].(map[string]any)["key"] != create["keys"].([]any)[0].(map[string]any)["key"] {
		t.Fatalf("bundle keys: %v", out["keys"])
	}

	// Rotation.
	next := l.encryptionKey(t, h.now)
	if status, out := h.call(t, d, token, http.MethodPut, "/links/"+l.id+"/keys", map[string]any{"keys": []any{next}}, true); status != http.StatusOK {
		t.Fatalf("rotate: %d %v", status, out)
	}
	_, out = h.call(t, nil, "", http.MethodGet, "/links/"+l.id, nil, false)
	if out["keys"].([]any)[0].(map[string]any)["key"] != next["key"] {
		t.Fatal("keys not rotated")
	}
	forged := newLinkKeys(t).encryptionKey(t, h.now) // signed by another link's key
	status, out = h.call(t, d, token, http.MethodPut, "/links/"+l.id+"/keys", map[string]any{"keys": []any{forged}}, true)
	wantError(t, status, out, http.StatusBadRequest, "invalid_link")

	// Revocation: afterwards the link looks like it never existed.
	if status, out := h.call(t, d, token, http.MethodDelete, "/links/"+l.id, nil, true); status != http.StatusOK {
		t.Fatalf("revoke: %d %v", status, out)
	}
	status, out = h.call(t, nil, "", http.MethodGet, "/links/"+l.id, nil, false)
	wantError(t, status, out, http.StatusNotFound, "not_found")
	status, out = h.call(t, nil, "", http.MethodGet, "/links/"+newLinkKeys(t).id, nil, false)
	wantError(t, status, out, http.StatusNotFound, "not_found")
	status, out = h.call(t, d, token, http.MethodDelete, "/links/"+l.id, nil, true)
	wantError(t, status, out, http.StatusNotFound, "not_found")
}

func TestLinkLimitAndOwnership(t *testing.T) {
	h := newHarness(t)
	h.server.InviteOnly = false
	d := newDevice(t)
	token := h.loggedIn(t, d, false)
	var first linkKeys
	for i := range links.MaxActive {
		l := newLinkKeys(t)
		if i == 0 {
			first = l
		}
		if status, out := h.call(t, d, token, http.MethodPost, "/links", l.create(t, h.now), true); status != http.StatusCreated {
			t.Fatalf("link %d: %d %v", i+1, status, out)
		}
	}
	status, out := h.call(t, d, token, http.MethodPost, "/links", newLinkKeys(t).create(t, h.now), true)
	wantError(t, status, out, http.StatusConflict, "link_limit")

	// Another Telegram user can't touch the first one's links: they answer
	// exactly like links that don't exist.
	other := newDevice(t)
	other.user = userID + 1
	otherToken := h.loggedIn(t, other, false)
	status, out = h.call(t, other, otherToken, http.MethodDelete, "/links/"+first.id, nil, true)
	wantError(t, status, out, http.StatusNotFound, "not_found")
	status, out = h.call(t, other, otherToken, http.MethodPut, "/links/"+first.id+"/keys",
		map[string]any{"keys": []any{first.encryptionKey(t, h.now)}}, true)
	wantError(t, status, out, http.StatusNotFound, "not_found")
	if status, _ := h.call(t, nil, "", http.MethodGet, "/links/"+first.id, nil, false); status != http.StatusOK {
		t.Fatal("the link was changed by another account")
	}
}
