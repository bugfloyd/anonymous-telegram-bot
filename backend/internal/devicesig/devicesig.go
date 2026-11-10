// Package devicesig checks signatures made with a device's P-256 key in the
// client (WebCrypto ECDSA with SHA-256).
//
// WebCrypto writes ECDSA signatures as r‖s (IEEE P1363, 64 bytes for P-256),
// not ASN.1 DER, and public keys as 65-byte uncompressed points.
//
// Every signed message is built by Message: a label naming its purpose, then
// length-prefixed parts. A signature made for one purpose can therefore never
// be accepted for another, and bytes can't be moved from one part to the next.
// The client builds messages the same way.
package devicesig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
)

// Labels for each kind of signed message.
const (
	LabelSession  = "incognitochat/v2/session"
	LabelRegister = "incognitochat/v2/register"
)

const (
	publicKeyLen = 65
	signatureLen = 64
)

var (
	// ErrPublicKey is returned for a malformed public key.
	ErrPublicKey = errors.New("devicesig: invalid public key")
	// ErrSignature is returned when a signature doesn't verify.
	ErrSignature = errors.New("devicesig: invalid signature")
)

// ParsePublicKey parses a 65-byte uncompressed P-256 point, as WebCrypto
// exports it in "raw" format. Points not on the curve are rejected.
func ParsePublicKey(raw []byte) (*ecdsa.PublicKey, error) {
	if len(raw) != publicKeyLen {
		return nil, ErrPublicKey
	}
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		return nil, ErrPublicKey
	}
	return pub, nil
}

// Message encodes a signed message: a 2-byte label length, the label, then
// each part as a 4-byte length followed by its bytes (big-endian lengths).
func Message(label string, parts ...[]byte) []byte {
	size := 2 + len(label)
	for _, p := range parts {
		size += 4 + len(p)
	}
	out := make([]byte, 0, size)
	out = binary.BigEndian.AppendUint16(out, uint16(len(label)))
	out = append(out, label...)
	for _, p := range parts {
		out = binary.BigEndian.AppendUint32(out, uint32(len(p)))
		out = append(out, p...)
	}
	return out
}

// Verify checks a P1363 signature over Message(label, parts...).
func Verify(pub *ecdsa.PublicKey, signature []byte, label string, parts ...[]byte) error {
	if pub == nil || len(signature) != signatureLen {
		return ErrSignature
	}
	digest := sha256.Sum256(Message(label, parts...))
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return ErrSignature
	}
	return nil
}
