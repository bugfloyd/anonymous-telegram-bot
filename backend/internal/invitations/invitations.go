// Package invitations holds the rules of invite-only access, carried over
// from v1 (design D5).
//
// While invite-only, owning a link needs an invitation; anyone can still
// create an account and write to links they were given. People invited by an
// admin are level 1, their invitees level 2, and so on. Each level has an
// allowance of invitations that admins can change at any time, and admins can
// grant individual members extra. Admins have no allowance limit.
//
// A code looks like "abcd-efgh" and can be used a set number of times. It is
// shared as t.me/<bot>/<app>?startapp=inv<code without the dash>.
package invitations

import (
	"crypto/rand"
	"math/big"
	"net/url"
	"strings"
)

const (
	// AdminMaxUses caps how many people one admin code can register.
	AdminMaxUses = 1000
	// MaxLevel bounds the levels admins can configure.
	MaxLevel = 100
	// MaxAllowance bounds a level's allowance and a single grant.
	MaxAllowance = 10000
	// RedeemsPerDay limits attempts to redeem a code, per account.
	RedeemsPerDay = 10

	// PayloadPrefix starts an invitation's start parameter.
	PayloadPrefix = "inv"
	// Alphabet leaves out characters that are easy to confuse (0/o, 1/l/i).
	Alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	half     = 4
)

// NewCode returns a random code in its canonical form, "abcd-efgh" (31^8,
// about 8.5e11 codes).
func NewCode() (string, error) {
	var b strings.Builder
	for i := range 2 * half {
		if i == half {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(Alphabet))))
		if err != nil {
			return "", err
		}
		b.WriteByte(Alphabet[n.Int64()])
	}
	return b.String(), nil
}

// Normalize accepts what people are likely to paste (any case, spaces, a
// missing dash, the start parameter or the whole invitation link) and returns
// the canonical code.
func Normalize(input string) (string, bool) {
	s := strings.TrimSpace(input)
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		s = u.Query().Get("startapp")
	}
	s = strings.ToLower(strings.Join(strings.Fields(s), ""))
	s = strings.TrimPrefix(s, PayloadPrefix)
	s = strings.ReplaceAll(s, "-", "")
	if len(s) != 2*half {
		return "", false
	}
	for _, c := range s {
		if !strings.ContainsRune(Alphabet, c) {
			return "", false
		}
	}
	return s[:half] + "-" + s[half:], true
}

// Payload is the start parameter of an invitation link.
func Payload(code string) string {
	return PayloadPrefix + strings.ReplaceAll(code, "-", "")
}

// Remaining is how many invitation uses a member can still put into codes.
func Remaining(levelAllowance, granted, allocated int) int {
	return max(0, levelAllowance+granted-allocated)
}
