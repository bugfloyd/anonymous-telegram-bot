package initdata

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

const botID = 123456789

var (
	now     = time.Unix(1_800_000_000, 0)
	testKey = ed25519.NewKeyFromSeed([]byte("0123456789abcdef0123456789abcdef"))
)

func verifier() Verifier {
	v := NewVerifier(botID)
	v.Key = testKey.Public().(ed25519.PublicKey)
	return v
}

// sign builds launch data the way Telegram does: URL-encoded fields, a
// signature over the data-check-string, and an (ignored) hash.
func sign(t *testing.T, fields map[string]string) string {
	t.Helper()
	sig := ed25519.Sign(testKey, []byte(CheckString(botID, fields)))
	q := url.Values{}
	for k, v := range fields {
		q.Set(k, v)
	}
	q.Set("hash", "ignored-hmac-from-the-bot-token")
	q.Set("signature", base64.RawURLEncoding.EncodeToString(sig))
	return q.Encode()
}

func fields(extra map[string]string) map[string]string {
	f := map[string]string{
		"auth_date":     "1799999990",
		"chat_instance": "-123",
		"chat_type":     "private",
		"user":          `{"id":42,"first_name":"Ab","last_name":"Cd","username":"ab_cd","language_code":"fa","allows_write_to_pm":true,"photo_url":"https://t.me/i/userpic/320/x.svg"}`,
	}
	for k, v := range extra {
		f[k] = v
	}
	return f
}

func TestVerify(t *testing.T) {
	raw := sign(t, fields(map[string]string{"start_param": "regABC_-123"}))
	got, err := verifier().Verify(raw, now)
	if err != nil {
		t.Fatal(err)
	}
	want := Data{UserID: 42, StartParam: "regABC_-123", AllowsWriteToPM: true, AuthDate: time.Unix(1799999990, 0), LanguageCode: "fa"}
	if got != want {
		t.Fatalf("Verify = %+v, want %+v", got, want)
	}
}

func TestVerifyWithoutStartParam(t *testing.T) {
	got, err := verifier().Verify(sign(t, fields(nil)), now)
	if err != nil || got.StartParam != "" {
		t.Fatalf("Verify = %+v, %v", got, err)
	}
}

func TestVerifyAcceptsPaddedSignature(t *testing.T) {
	raw := sign(t, fields(nil))
	q, _ := url.ParseQuery(raw)
	q.Set("signature", q.Get("signature")+"==")
	if _, err := verifier().Verify(q.Encode(), now); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejects(t *testing.T) {
	good := sign(t, fields(nil))
	tamper := func(key, value string) string {
		q, _ := url.ParseQuery(good)
		q.Set(key, value)
		return q.Encode()
	}

	tests := []struct {
		name string
		raw  string
		v    Verifier
		want error
	}{
		{"tampered user", tamper("user", `{"id":43}`), verifier(), ErrSignature},
		{"added field", good + "&start_param=reg", verifier(), ErrSignature},
		{"hash alone is not enough", strings.Split(good, "&signature=")[0], verifier(), ErrMalformed},
		{"duplicate field", good + "&auth_date=1799999990", verifier(), ErrMalformed},
		{"empty key", good + "&=x", verifier(), ErrMalformed},
		{"bad escape", good + "&x=%zz", verifier(), ErrMalformed},
		{"other bot", good, func() Verifier { v := verifier(); v.BotID = 987; return v }(), ErrSignature},
		{"production key", good, NewVerifier(botID), ErrSignature},
		{"no key", good, Verifier{BotID: botID}, ErrSignature},
		{"short signature", tamper("signature", "AAAA"), verifier(), ErrMalformed},
		{"too old", sign(t, fields(map[string]string{"auth_date": "1799999399"})), verifier(), ErrExpired},
		{"from the future", sign(t, fields(map[string]string{"auth_date": "1800000061"})), verifier(), ErrExpired},
		{"non-numeric date", sign(t, fields(map[string]string{"auth_date": "soon"})), verifier(), ErrMalformed},
		{"no user", sign(t, map[string]string{"auth_date": "1799999990"}), verifier(), ErrMalformed},
		{"user without id", sign(t, fields(map[string]string{"user": `{"first_name":"x"}`})), verifier(), ErrMalformed},
		{"negative id", sign(t, fields(map[string]string{"user": `{"id":-5}`})), verifier(), ErrMalformed},
		{"fractional id", sign(t, fields(map[string]string{"user": `{"id":4.2}`})), verifier(), ErrMalformed},
		{"bad start param", sign(t, fields(map[string]string{"start_param": "a b"})), verifier(), ErrMalformed},
		{"long start param", sign(t, fields(map[string]string{"start_param": strings.Repeat("a", 513)})), verifier(), ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := tt.v.Verify(tt.raw, now); !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestCheckString(t *testing.T) {
	got := CheckString(7, map[string]string{"b": "2", "a": "1", "hash": "x", "signature": "y", "chat_instance": "3", "chat": "4"})
	want := "7:WebAppData\na=1\nb=2\nchat=4\nchat_instance=3"
	if got != want {
		t.Fatalf("CheckString =\n%s\nwant\n%s", got, want)
	}
}
