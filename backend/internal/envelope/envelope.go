// Package envelope checks the layout of message envelopes (docs/protocol.md).
// The server can't read envelopes; it only refuses ones that can't be valid,
// so storage holds nothing but well-formed ciphertext of the expected sizes.
//
//	envelope = suite (1) ‖ wrap count (1, 1–16) ‖ wraps (121 each) ‖ body
//	body     = nonce (12) ‖ ciphertext of a padded inner message ‖ tag (16)
package envelope

import (
	"errors"
	"slices"
)

const (
	// Suite is the only algorithm suite.
	Suite = 0x01
	// MaxWraps bounds how many recipient keys one envelope has.
	MaxWraps = 16
	// WrapLen is a wrap's size: key ID (8), HPKE enc (65), wrapped key (48).
	WrapLen  = 8 + 65 + 48
	nonceLen = 12
	tagLen   = 16
)

// PaddedSizes are the sizes inner messages are padded to.
var PaddedSizes = []int{512, 2048, 8192, 16384}

// MaxLen is the largest valid envelope.
var MaxLen = 2 + MaxWraps*WrapLen + nonceLen + PaddedSizes[len(PaddedSizes)-1] + tagLen

// ErrInvalid is returned for an envelope that can't be valid.
var ErrInvalid = errors.New("envelope: invalid")

// Check accepts an envelope whose layout is valid and returns its wrap count.
func Check(env []byte) (int, error) {
	if len(env) < 2 || len(env) > MaxLen || env[0] != Suite {
		return 0, ErrInvalid
	}
	wraps := int(env[1])
	if wraps < 1 || wraps > MaxWraps {
		return 0, ErrInvalid
	}
	body := len(env) - 2 - wraps*WrapLen
	if !slices.Contains(PaddedSizes, body-nonceLen-tagLen) {
		return 0, ErrInvalid
	}
	return wraps, nil
}
