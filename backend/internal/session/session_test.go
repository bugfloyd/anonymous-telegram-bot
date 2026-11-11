package session

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

var (
	now     = time.Unix(1_800_000_000, 0)
	keyA    = []byte("0123456789abcdef0123456789abcdef")
	keyB    = []byte("fedcba9876543210fedcba9876543210")
	account = ids.New()
	device  = ids.New()
)

func issuer(t *testing.T, keys map[byte][]byte, current byte) *Issuer {
	t.Helper()
	i, err := NewIssuer(keys, current)
	if err != nil {
		t.Fatal(err)
	}
	return i
}

func TestIssueVerify(t *testing.T) {
	i := issuer(t, map[byte][]byte{1: keyA}, 1)
	token, expires, err := i.Issue(account, device, now)
	if err != nil {
		t.Fatal(err)
	}
	if !expires.Equal(now.Add(TTL)) {
		t.Fatalf("expires = %v", expires)
	}
	claims, err := i.Verify(token, now.Add(TTL-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if claims.AccountID != account || claims.DeviceID != device || !claims.Expires.Equal(expires) {
		t.Fatalf("claims = %+v", claims)
	}
	if _, err := i.Verify(token, now.Add(TTL)); !errors.Is(err, ErrToken) {
		t.Fatalf("expired token: err = %v", err)
	}
}

func TestKeyRotation(t *testing.T) {
	old := issuer(t, map[byte][]byte{1: keyA}, 1)
	token, _, _ := old.Issue(account, device, now)

	rotated := issuer(t, map[byte][]byte{1: keyA, 2: keyB}, 2)
	if _, err := rotated.Verify(token, now); err != nil {
		t.Fatalf("token signed with the previous key: %v", err)
	}
	newToken, _, _ := rotated.Issue(account, device, now)
	if _, err := old.Verify(newToken, now); !errors.Is(err, ErrToken) {
		t.Fatalf("token from an unknown key: err = %v", err)
	}
	retired := issuer(t, map[byte][]byte{2: keyB}, 2)
	if _, err := retired.Verify(token, now); !errors.Is(err, ErrToken) {
		t.Fatalf("token from a retired key: err = %v", err)
	}
}

func TestVerifyRejectsTampering(t *testing.T) {
	i := issuer(t, map[byte][]byte{1: keyA}, 1)
	token, _, _ := i.Issue(account, device, now)
	other := issuer(t, map[byte][]byte{1: keyB}, 1)
	if _, err := other.Verify(token, now); !errors.Is(err, ErrToken) {
		t.Fatal("accepted a token from another key with the same ID")
	}
	payload, mac, _ := strings.Cut(strings.TrimPrefix(token, tokenPrefix), ".")
	flip := func(s string, at int) string {
		b := []byte(s)
		if b[at] == 'A' {
			b[at] = 'B'
		} else {
			b[at] = 'A'
		}
		return string(b)
	}
	for name, bad := range map[string]string{
		"empty":            "",
		"no prefix":        strings.TrimPrefix(token, tokenPrefix),
		"other version":    "v2." + payload + "." + mac,
		"payload changed":  tokenPrefix + flip(payload, 20) + "." + mac,
		"mac changed":      tokenPrefix + payload + "." + flip(mac, 5),
		"no mac":           tokenPrefix + payload,
		"padded":           token + "=",
		"short payload":    tokenPrefix + payload[:10] + "." + mac,
		"trailing garbage": token + ".x",
	} {
		if _, err := i.Verify(bad, now); !errors.Is(err, ErrToken) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestNewIssuerRejects(t *testing.T) {
	if _, err := NewIssuer(map[byte][]byte{1: keyA}, 2); !errors.Is(err, ErrKeys) {
		t.Error("accepted a missing current key")
	}
	if _, err := NewIssuer(map[byte][]byte{1: []byte("short")}, 1); !errors.Is(err, ErrKeys) {
		t.Error("accepted a short key")
	}
}

func TestIssueRejectsBadIDs(t *testing.T) {
	i := issuer(t, map[byte][]byte{1: keyA}, 1)
	if _, _, err := i.Issue("nope", device, now); err == nil {
		t.Error("accepted a bad account ID")
	}
	if _, _, err := i.Issue(account, "nope", now); err == nil {
		t.Error("accepted a bad device ID")
	}
}

// signLogin signs a login the way the client does.
func signLogin(t *testing.T, key *ecdsa.PrivateKey, login Login) Login {
	t.Helper()
	msg := devicesig.Message(devicesig.LabelSession,
		[]byte(login.AccountID), []byte(login.DeviceID), []byte(strconv.FormatInt(login.Timestamp, 10)), []byte(login.Nonce))
	digest := sha256.Sum256(msg)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	login.Signature = make([]byte, 64)
	r.FillBytes(login.Signature[:32])
	s.FillBytes(login.Signature[32:])
	return login
}

func TestCheck(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	login := signLogin(t, key, Login{AccountID: account, DeviceID: device, Timestamp: now.Unix(), Nonce: "nonce-0123456789abc"})

	nonces := NewMemoryNonces(func() time.Time { return now })
	if err := Check(ctx, login, &key.PublicKey, nonces, now); err != nil {
		t.Fatalf("valid login refused: %v", err)
	}
	if err := Check(ctx, login, &key.PublicKey, nonces, now); !errors.Is(err, ErrLogin) {
		t.Fatalf("replayed login: err = %v", err)
	}

	later := func(d time.Duration) *MemoryNonces { return NewMemoryNonces(func() time.Time { return now.Add(d) }) }
	if err := Check(ctx, login, &key.PublicKey, later(0), now.Add(MaxSkew+time.Second)); !errors.Is(err, ErrLogin) {
		t.Fatal("accepted a stale timestamp")
	}
	if err := Check(ctx, login, &key.PublicKey, later(0), now.Add(-MaxSkew-time.Second)); !errors.Is(err, ErrLogin) {
		t.Fatal("accepted a timestamp from the future")
	}

	other, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	fresh := NewMemoryNonces(func() time.Time { return now })
	if err := Check(ctx, login, &other.PublicKey, fresh, now); !errors.Is(err, ErrLogin) {
		t.Fatal("accepted another device's signature")
	}
	// The refused login must not have used up the nonce.
	if err := Check(ctx, login, &key.PublicKey, fresh, now); err != nil {
		t.Fatalf("nonce was consumed by a refused login: %v", err)
	}

	for name, bad := range map[string]Login{
		"other account ID": {AccountID: ids.New(), DeviceID: device, Timestamp: login.Timestamp, Nonce: login.Nonce, Signature: login.Signature},
		"other device ID":  {AccountID: account, DeviceID: ids.New(), Timestamp: login.Timestamp, Nonce: login.Nonce, Signature: login.Signature},
		"other nonce":      {AccountID: account, DeviceID: device, Timestamp: login.Timestamp, Nonce: "nonce-9999999999abc", Signature: login.Signature},
		"bad account ID":   {AccountID: "x", DeviceID: device, Timestamp: login.Timestamp, Nonce: login.Nonce, Signature: login.Signature},
		"bad device ID":    {AccountID: account, DeviceID: "x", Timestamp: login.Timestamp, Nonce: login.Nonce, Signature: login.Signature},
		"short nonce":      {AccountID: account, DeviceID: device, Timestamp: login.Timestamp, Nonce: "short", Signature: login.Signature},
		"nonce charset":    {AccountID: account, DeviceID: device, Timestamp: login.Timestamp, Nonce: "nonce with spaces!!", Signature: login.Signature},
	} {
		if err := Check(ctx, bad, &key.PublicKey, NewMemoryNonces(func() time.Time { return now }), now); !errors.Is(err, ErrLogin) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestMemoryNoncesExpire(t *testing.T) {
	clock := now
	m := NewMemoryNonces(func() time.Time { return clock })
	ctx := context.Background()
	if ok, _ := m.Use(ctx, "n", now.Add(time.Minute)); !ok {
		t.Fatal("first use refused")
	}
	if ok, _ := m.Use(ctx, "n", now.Add(time.Minute)); ok {
		t.Fatal("second use accepted")
	}
	clock = now.Add(time.Minute)
	if ok, _ := m.Use(ctx, "n", clock.Add(time.Minute)); !ok {
		t.Fatal("use after expiry refused")
	}
}
