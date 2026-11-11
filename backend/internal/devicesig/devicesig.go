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
	"encoding/base64"
	"encoding/binary"
	"errors"
	"math/big"
)

// Labels for each kind of signed message.
const (
	LabelSession     = "incognitochat/v2/session"
	LabelRegister    = "incognitochat/v2/register"
	LabelDeviceList  = "incognitochat/v2/device-list"
	LabelFingerprint = "incognitochat/v2/fingerprint"
)

// Suite is the algorithm suite (P-256, HKDF-SHA256, AES-256-GCM). It's part
// of every fingerprint, so a later suite can never produce the same one.
const Suite = 0x01

const (
	publicKeyLen = 65
	signatureLen = 64
)

var (
	// ErrPublicKey is returned for a malformed public key.
	ErrPublicKey = errors.New("devicesig: invalid public key")
	// ErrSignature is returned when a signature doesn't verify.
	ErrSignature = errors.New("devicesig: invalid signature")
	// ErrMessage is returned by Parse for bytes Message can't have produced.
	ErrMessage = errors.New("devicesig: malformed message")
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

// Parse splits an encoded message back into its label and parts. Only the
// exact encoding is accepted: no lengths past the end, nothing after the
// last part.
func Parse(msg []byte) (string, [][]byte, error) {
	if len(msg) < 2 {
		return "", nil, ErrMessage
	}
	n := int(binary.BigEndian.Uint16(msg))
	msg = msg[2:]
	if len(msg) < n {
		return "", nil, ErrMessage
	}
	label := string(msg[:n])
	msg = msg[n:]
	var parts [][]byte
	for len(msg) > 0 {
		if len(msg) < 4 {
			return "", nil, ErrMessage
		}
		size := binary.BigEndian.Uint32(msg)
		msg = msg[4:]
		if uint64(len(msg)) < uint64(size) {
			return "", nil, ErrMessage
		}
		parts = append(parts, msg[:size:size])
		msg = msg[size:]
	}
	return label, parts, nil
}

// Fingerprint is a public key's fingerprint, as the client computes it: the
// SHA-256 of Message(LabelFingerprint, [Suite], raw), as 43 base64url
// characters. raw is the 65-byte point.
func Fingerprint(raw []byte) string {
	digest := sha256.Sum256(Message(LabelFingerprint, []byte{Suite}, raw))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}

// Verify checks a P1363 signature over Message(label, parts...).
func Verify(pub *ecdsa.PublicKey, signature []byte, label string, parts ...[]byte) error {
	return VerifyEncoded(pub, signature, Message(label, parts...))
}

// VerifyEncoded checks a P1363 signature over an already encoded message.
func VerifyEncoded(pub *ecdsa.PublicKey, signature, msg []byte) error {
	if pub == nil || len(signature) != signatureLen {
		return ErrSignature
	}
	digest := sha256.Sum256(msg)
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	if !ecdsa.Verify(pub, digest[:], r, s) {
		return ErrSignature
	}
	return nil
}
