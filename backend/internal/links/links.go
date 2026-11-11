// Package links checks link bundles (design §8).
//
// A link is t.me/<bot>/<app>?startapp=1<link ID><fingerprint>. Its bundle,
// which anyone with the link ID can fetch, holds the link signing key (whose
// fingerprint is in the link) and the current encryption keys, each signed by
// the signing key with a validity window. Senders check all of that before
// encrypting, so a server that swaps keys is caught. The server only checks
// that a bundle is well formed and signed, and stores it.
//
// An encryption key's signature covers devicesig.Message(LabelLinkKey,
// suite, key, from, until), with times as 8-byte big-endian Unix seconds on a
// whole hour. Signing keys are never reused between links, so the link ID
// needn't be signed.
package links

import (
	"crypto/ecdsa"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
)

const (
	// MaxActive is how many links one account can have at a time: one, which
	// can be replaced (a new address, the same identity).
	MaxActive = 1
	// MaxKeys is how many encryption keys a bundle can list (v2.0 uses one,
	// two while rotating).
	MaxKeys = 4
	// MaxKeyValidity is the longest an encryption key may be offered for.
	MaxKeyValidity = 8 * 7 * 24 * time.Hour

	idBytes = 8
)

// ErrInvalid is returned for a malformed or badly signed bundle.
var ErrInvalid = errors.New("links: invalid bundle")

// Key is one encryption key in a bundle.
type Key struct {
	Key       []byte // 65-byte P-256 point (ECDH)
	From      time.Time
	Until     time.Time
	Signature []byte // by the link signing key
}

// ValidID reports whether id is a link ID: 8 bytes as 11 base64url characters.
func ValidID(id string) bool {
	b, err := base64.RawURLEncoding.Strict().DecodeString(id)
	return err == nil && len(b) == idBytes
}

// KeyParts are the signed parts of an encryption key.
func KeyParts(k Key) [][]byte {
	return [][]byte{
		{devicesig.Suite},
		k.Key,
		binary.BigEndian.AppendUint64(nil, uint64(k.From.Unix())),
		binary.BigEndian.AppendUint64(nil, uint64(k.Until.Unix())),
	}
}

// Check verifies a bundle's encryption keys against its signing key: one to
// MaxKeys keys, each a valid point, on whole hours, valid for at most
// MaxKeyValidity, not yet expired, and signed.
func Check(signingKey *ecdsa.PublicKey, keys []Key, now time.Time) error {
	if len(keys) == 0 || len(keys) > MaxKeys {
		return ErrInvalid
	}
	for _, k := range keys {
		if _, err := devicesig.ParsePublicKey(k.Key); err != nil {
			return ErrInvalid
		}
		if !onHour(k.From) || !onHour(k.Until) || !k.From.Before(k.Until) ||
			k.Until.Sub(k.From) > MaxKeyValidity || !k.Until.After(now) {
			return ErrInvalid
		}
		if devicesig.Verify(signingKey, k.Signature, devicesig.LabelLinkKey, KeyParts(k)...) != nil {
			return ErrInvalid
		}
	}
	return nil
}

func onHour(t time.Time) bool {
	return t.Unix() > 0 && t.Unix()%3600 == 0
}
