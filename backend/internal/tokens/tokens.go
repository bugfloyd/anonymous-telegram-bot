// Package tokens issues and checks anonymous send tokens (design §11):
// RSA blind signatures (RFC 9474, RSABSSA-SHA384-PSS-Deterministic), in the
// shape of Privacy Pass token type 2 (RFC 9578).
//
// A logged-in client gets a batch of blinded tokens signed. Unblinded, each
// is an ordinary RSA-PSS signature the issuer can't link to the request that
// produced it, so spending one on a send (without logging in) doesn't say who
// sent it. Each token is spent once.
//
//	token       = type (2) ‖ nonce (32) ‖ challenge (32) ‖ key ID (32) ‖ authenticator (k)
//	challenge   = SHA-256(devicesig.Message("incognitochat/v2/send-token", [suite], u64(epoch)))
//	key ID      = SHA-256(the issuer key's SubjectPublicKeyInfo, DER)
//
// Epochs are weeks. A token is accepted in its own epoch and the next one,
// so spent tokens are only remembered for two weeks and the key can stay the
// same: clients spend this week's tokens or last week's.
package tokens

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"time"

	"github.com/cloudflare/circl/blindsign/blindrsa"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
)

const (
	// Type is the Privacy Pass token type for blind RSA (RFC 9578).
	Type = 0x0002
	// Epoch is how long tokens of one challenge are minted for.
	Epoch = 7 * 24 * time.Hour
	// ModulusBits is the issuer key size for token type 2.
	ModulusBits = 2048

	labelChallenge = "incognitochat/v2/send-token"
	inputLen       = 2 + 32 + 32 + 32
)

var (
	// ErrInvalid is returned for a token that isn't valid now.
	ErrInvalid = errors.New("tokens: invalid token")
	// ErrKey is returned for an unusable issuer key.
	ErrKey = errors.New("tokens: invalid issuer key")
)

// Issuer holds the issuer key.
type Issuer struct {
	signer   blindrsa.Signer
	verifier blindrsa.Verifier
	public   *rsa.PublicKey
	keyID    [32]byte
	k        int
}

// NewIssuer reads a PKCS#8 PEM RSA private key of ModulusBits bits.
func NewIssuer(pemKey []byte) (*Issuer, error) {
	block, _ := pem.Decode(pemKey)
	if block == nil || block.Type != "PRIVATE KEY" {
		return nil, ErrKey
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, ErrKey
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok || key.N.BitLen() != ModulusBits || key.Validate() != nil {
		return nil, ErrKey
	}
	id, err := KeyID(&key.PublicKey)
	if err != nil {
		return nil, ErrKey
	}
	verifier, err := blindrsa.NewVerifier(blindrsa.SHA384PSSDeterministic, &key.PublicKey)
	if err != nil {
		return nil, ErrKey
	}
	return &Issuer{signer: blindrsa.NewSigner(key), verifier: verifier, public: &key.PublicKey, keyID: id, k: key.Size()}, nil
}

// KeyID is the SHA-256 of a public key's SubjectPublicKeyInfo (DER).
func KeyID(pub *rsa.PublicKey) ([32]byte, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(der), nil
}

// PublicKey returns the issuer's public key.
func (i *Issuer) PublicKey() *rsa.PublicKey { return i.public }

// KeyID returns the issuer key's ID.
func (i *Issuer) KeyID() [32]byte { return i.keyID }

// BlindedLen is the length of a blinded message and of its signature.
func (i *Issuer) BlindedLen() int { return i.k }

// Sign signs a blinded message. It never sees what it signs.
func (i *Issuer) Sign(blinded []byte) ([]byte, error) {
	if len(blinded) != i.k {
		return nil, ErrInvalid
	}
	return i.signer.BlindSign(blinded)
}

// EpochOf returns the epoch a time falls in.
func EpochOf(t time.Time) uint64 {
	return uint64(t.Unix() / int64(Epoch/time.Second))
}

// Challenge returns the challenge digest of an epoch.
func Challenge(epoch uint64) [32]byte {
	return sha256.Sum256(devicesig.Message(labelChallenge, []byte{devicesig.Suite}, binary.BigEndian.AppendUint64(nil, epoch)))
}

// Check verifies a token that is valid now (its epoch is this one or the
// previous one) and returns its nonce, to be spent, and when the nonce can be
// forgotten (the end of the last epoch the token is accepted in).
func (i *Issuer) Check(token []byte, now time.Time) (nonce []byte, forgetAfter time.Time, err error) {
	if len(token) != inputLen+i.k || binary.BigEndian.Uint16(token) != Type {
		return nil, time.Time{}, ErrInvalid
	}
	if [32]byte(token[66:98]) != i.keyID {
		return nil, time.Time{}, ErrInvalid
	}
	current := EpochOf(now)
	challenge := [32]byte(token[34:66])
	var epoch uint64
	switch challenge {
	case Challenge(current):
		epoch = current
	case Challenge(current - 1):
		epoch = current - 1
	default:
		return nil, time.Time{}, ErrInvalid
	}
	if i.verifier.Verify(token[:inputLen], token[inputLen:]) != nil {
		return nil, time.Time{}, ErrInvalid
	}
	end := time.Unix(int64(epoch+2)*int64(Epoch/time.Second), 0)
	return token[2:34], end, nil
}
