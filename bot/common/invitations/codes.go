package invitations

import (
	"crypto/rand"
	"math/big"
	"net/url"
	"strings"
)

// codeAlphabet leaves out characters that are easy to confuse (0/o, 1/l/i).
const codeAlphabet = "abcdefghjkmnpqrstuvwxyz23456789"

// codeHalfLength: codes look like "abcd-efgh" (31^8 ≈ 8.5e11 combinations).
const codeHalfLength = 4

// linkPrefix marks invitation codes in t.me/<bot>?start=<payload> links. Other
// start payloads (user links) never contain a dash, so they can't collide.
const linkPrefix = "inv-"

// NewCode returns a random invitation code from a cryptographic source.
func NewCode() (string, error) {
	var b strings.Builder
	for i := 0; i < 2*codeHalfLength; i++ {
		if i == codeHalfLength {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			return "", err
		}
		b.WriteByte(codeAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// LinkPayload is the /start payload for an invitation link.
func LinkPayload(code string) string {
	return linkPrefix + code
}

// CodeFromPayload extracts the code from a /start payload, if it is an invitation.
func CodeFromPayload(payload string) (string, bool) {
	if !strings.HasPrefix(payload, linkPrefix) {
		return "", false
	}
	return NormalizeCode(strings.TrimPrefix(payload, linkPrefix))
}

// NormalizeCode accepts what users are likely to paste (any case, spaces, a
// missing dash, the invitation link itself) and returns the canonical code.
func NormalizeCode(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = u.Query().Get("start")
	}
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	s = strings.TrimPrefix(s, linkPrefix)
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 2*codeHalfLength {
		return "", false
	}
	for _, c := range s {
		if !strings.ContainsRune(codeAlphabet, c) {
			return "", false
		}
	}
	return s[:codeHalfLength] + "-" + s[codeHalfLength:], true
}
