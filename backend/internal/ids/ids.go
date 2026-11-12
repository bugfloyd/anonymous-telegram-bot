// Package ids creates and checks the random identifiers used for accounts,
// devices, links and mailboxes: 128 random bits, written as 22 base64url
// characters without padding. They carry no meaning and no time.
package ids

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
)

// Len is the length of an ID in characters.
const Len = 22

var encoding = base64.RawURLEncoding.Strict()

// ErrInvalid is returned for anything that isn't a well-formed ID.
var ErrInvalid = errors.New("ids: invalid id")

// New returns a new random ID.
func New() string {
	var b [16]byte
	_, _ = rand.Read(b[:]) // crypto/rand.Read never fails (Go 1.24+).
	return encoding.EncodeToString(b[:])
}

// Decode returns the 16 bytes of an ID.
func Decode(id string) ([16]byte, error) {
	var out [16]byte
	if len(id) != Len {
		return out, ErrInvalid
	}
	b, err := encoding.DecodeString(id)
	if err != nil || len(b) != len(out) {
		return out, ErrInvalid
	}
	copy(out[:], b)
	return out, nil
}

// Encode returns the ID for 16 bytes.
func Encode(b [16]byte) string {
	return encoding.EncodeToString(b[:])
}

// Valid reports whether id is a well-formed ID, in its one canonical spelling.
func Valid(id string) bool {
	_, err := Decode(id)
	return err == nil
}
