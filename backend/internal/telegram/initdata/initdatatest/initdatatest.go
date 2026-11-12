// Package initdatatest builds signed Telegram launch data for tests.
package initdatatest

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/url"

	"github.com/incognitochat/incognitochat/backend/internal/telegram/initdata"
)

// Key is a fixed test key standing in for Telegram's.
var Key = ed25519.NewKeyFromSeed([]byte("0123456789abcdef0123456789abcdef"))

// Verifier accepts launch data signed by Key for botID.
func Verifier(botID int64) initdata.Verifier {
	v := initdata.NewVerifier(botID)
	v.Key = Key.Public().(ed25519.PublicKey)
	return v
}

// Sign builds launch data the way Telegram does: URL-encoded fields, a
// signature over the data-check-string, and an (ignored) hash.
func Sign(botID int64, fields map[string]string) string {
	sig := ed25519.Sign(Key, []byte(initdata.CheckString(botID, fields)))
	q := url.Values{}
	for k, v := range fields {
		q.Set(k, v)
	}
	q.Set("hash", "ignored-hmac-from-the-bot-token")
	q.Set("signature", base64.RawURLEncoding.EncodeToString(sig))
	return q.Encode()
}
