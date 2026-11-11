// Package session handles device logins and the short-lived tokens they get.
//
// A device logs in by signing (account ID, device ID, timestamp, nonce) with its own P-256
// key. The server checks the timestamp is fresh, the signature, and that the
// nonce hasn't been used, then issues a token valid for TTL. Tokens are an
// HMAC over the account, the device and an expiry, so checking one needs no
// database read. Signing keys have a one-byte ID so they can be rotated: the
// current key signs, older keys still verify until their tokens expire.
package session

import (
	"context"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
)

const (
	// TTL is how long a session token is valid.
	TTL = 15 * time.Minute
	// MaxSkew is how far a login timestamp may be from the server's clock.
	MaxSkew = 60 * time.Second
	// NonceTTL is how long a used nonce is remembered: long enough to cover
	// the whole window in which its timestamp is accepted.
	NonceTTL = 2*MaxSkew + 10*time.Second

	tokenPrefix = "v1."
	macLabel    = "incognitochat/v2/session-token"
	minKeyLen   = 32
)

var (
	// ErrToken is returned for any token that isn't valid now.
	ErrToken = errors.New("session: invalid token")
	// ErrLogin is returned for a login that must be refused.
	ErrLogin = errors.New("session: login refused")
	// ErrKeys is returned for an unusable key configuration.
	ErrKeys = errors.New("session: invalid keys")
)

// Claims is what a valid token says.
type Claims struct {
	AccountID string
	DeviceID  string
	Expires   time.Time
}

// Issuer issues and checks tokens.
type Issuer struct {
	keys    map[byte][]byte
	current byte
}

// NewIssuer returns an issuer that signs with keys[current] and accepts any key
// in keys. Every key must be at least 32 bytes.
func NewIssuer(keys map[byte][]byte, current byte) (*Issuer, error) {
	if _, ok := keys[current]; !ok {
		return nil, ErrKeys
	}
	copied := make(map[byte][]byte, len(keys))
	for id, k := range keys {
		if len(k) < minKeyLen {
			return nil, ErrKeys
		}
		copied[id] = append([]byte(nil), k...)
	}
	return &Issuer{keys: copied, current: current}, nil
}

// Issue returns a token for the device, and when it expires.
func (i *Issuer) Issue(accountID, deviceID string, now time.Time) (string, time.Time, error) {
	account, err := ids.Decode(accountID)
	if err != nil {
		return "", time.Time{}, err
	}
	device, err := ids.Decode(deviceID)
	if err != nil {
		return "", time.Time{}, err
	}
	expires := now.Add(TTL).Truncate(time.Second)
	payload := make([]byte, 0, 1+8+16+16)
	payload = append(payload, i.current)
	payload = binary.BigEndian.AppendUint64(payload, uint64(expires.Unix()))
	payload = append(payload, account[:]...)
	payload = append(payload, device[:]...)
	mac := i.mac(i.keys[i.current], payload)
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac)
	return token, expires, nil
}

// Verify checks a token and returns its claims.
func (i *Issuer) Verify(token string, now time.Time) (Claims, error) {
	rest, ok := strings.CutPrefix(token, tokenPrefix)
	if !ok {
		return Claims{}, ErrToken
	}
	encPayload, encMAC, ok := strings.Cut(rest, ".")
	if !ok {
		return Claims{}, ErrToken
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(encPayload)
	if err != nil || len(payload) != 1+8+16+16 {
		return Claims{}, ErrToken
	}
	mac, err := base64.RawURLEncoding.Strict().DecodeString(encMAC)
	if err != nil {
		return Claims{}, ErrToken
	}
	key, ok := i.keys[payload[0]]
	if !ok || !hmac.Equal(mac, i.mac(key, payload)) {
		return Claims{}, ErrToken
	}
	expires := time.Unix(int64(binary.BigEndian.Uint64(payload[1:9])), 0)
	if !now.Before(expires) {
		return Claims{}, ErrToken
	}
	var account, device [16]byte
	copy(account[:], payload[9:25])
	copy(device[:], payload[25:41])
	return Claims{AccountID: ids.Encode(account), DeviceID: ids.Encode(device), Expires: expires}, nil
}

func (i *Issuer) mac(key, payload []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(macLabel))
	h.Write(payload)
	return h.Sum(nil)
}

// NonceStore remembers used login nonces.
type NonceStore interface {
	// Use records nonce until expires. It reports false if the nonce was
	// already recorded and hasn't expired.
	Use(ctx context.Context, nonce string, expires time.Time) (bool, error)
}

// Login is a device's signed login request.
type Login struct {
	AccountID string
	DeviceID  string
	Timestamp int64 // Unix seconds
	Nonce     string
	Signature []byte
}

// Check verifies a login against the device's public key. The nonce is only
// recorded once the signature is valid, so unsigned requests can't fill the
// nonce store.
func Check(ctx context.Context, login Login, pub *ecdsa.PublicKey, nonces NonceStore, now time.Time) error {
	if !ids.Valid(login.AccountID) || !ids.Valid(login.DeviceID) || !validNonce(login.Nonce) {
		return ErrLogin
	}
	at := time.Unix(login.Timestamp, 0)
	if at.Before(now.Add(-MaxSkew)) || at.After(now.Add(MaxSkew)) {
		return ErrLogin
	}
	if devicesig.Verify(pub, login.Signature, devicesig.LabelSession, LoginParts(login)...) != nil {
		return ErrLogin
	}
	fresh, err := nonces.Use(ctx, login.Nonce, now.Add(NonceTTL))
	if err != nil {
		return err
	}
	if !fresh {
		return ErrLogin
	}
	return nil
}

// LoginParts are the signed parts of a login: account ID, device ID, the
// timestamp in decimal, and the nonce.
func LoginParts(login Login) [][]byte {
	return [][]byte{
		[]byte(login.AccountID),
		[]byte(login.DeviceID),
		[]byte(strconv.FormatInt(login.Timestamp, 10)),
		[]byte(login.Nonce),
	}
}

// validNonce accepts 16 to 64 base64url characters (96 to 384 bits).
func validNonce(nonce string) bool {
	if len(nonce) < 16 || len(nonce) > 64 {
		return false
	}
	for _, c := range nonce {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
