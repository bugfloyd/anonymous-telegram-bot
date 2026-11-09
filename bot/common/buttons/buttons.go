// Package buttons seals the data of buttons that refer to another user.
//
// Button data is readable by any modified Telegram client, and such a client
// can also send any data it likes. So the data of a button that refers to
// another user never says who that is, and the bot only acts on data it
// sealed for the person pressing the button:
//
//	<action>|base64url(tag ‖ ciphertext)
//
// The plaintext is a Ref (the other user's UUID, their role in the
// conversation and their message ID). It is sealed for the user who sees the
// button, the viewer, with a deterministic authenticated encryption (the SIV
// construction):
//
//	tag        = HMAC-SHA256(macKey, action ‖ viewerUUID ‖ plaintext), first 16 bytes
//	ciphertext = AES-256-CTR(encKey, iv = tag, plaintext)
//
// Opening the data as another user or for another action fails the tag
// check. Because the viewer is part of the tag, the same person looks
// different in every viewer's buttons, so viewers can't compare notes. Both
// keys are derived from the bot's identity key (see the secrets package).
package buttons

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"

	"github.com/bugfloyd/telegram-incognito-bot/common/conversation"
	"github.com/google/uuid"
)

// MaxDataLength is Telegram's limit for callback data.
const MaxDataLength = 64

// MinKeyLength is the minimum length of the identity key.
const MinKeyLength = 32

// Action is what pressing a button does. It is also the readable prefix of the
// button's data, which the dispatcher routes on.
type Action string

const (
	OpenMessage Action = "o"
	Reply       Action = "r"
	Block       Action = "b"
	Unblock     Action = "ub"
)

// Ref is what a button refers to: a message from another user.
type Ref struct {
	// Other is the other user's UUID.
	Other string
	// Role is the other user's role in this conversation.
	Role conversation.Role
	// MessageID is the other user's message, in their own chat with the bot.
	MessageID int64
}

// ErrInvalid is returned for button data that wasn't sealed by the bot for
// this viewer and action.
var ErrInvalid = errors.New("invalid button data")

const (
	tagSize       = 16
	plaintextSize = 16 + 1 + 8 // UUID, role, message ID
)

// Strict, so every sealed value has exactly one valid encoding.
var encoding = base64.RawURLEncoding.Strict()

// Keys seal and open button data. They are safe for concurrent use.
type Keys struct {
	enc cipher.Block
	mac []byte
}

// NewKeys derives the button keys from the identity key.
func NewKeys(identityKey []byte) (*Keys, error) {
	if len(identityKey) < MinKeyLength {
		return nil, fmt.Errorf("identity key must be at least %d bytes, got %d", MinKeyLength, len(identityKey))
	}
	enc, err := aes.NewCipher(derive(identityKey, "button-enc-v1"))
	if err != nil {
		return nil, fmt.Errorf("failed to create button cipher: %w", err)
	}
	return &Keys{enc: enc, mac: derive(identityKey, "button-mac-v1")}, nil
}

// derive returns a 32-byte key for one purpose: HMAC-SHA256(identityKey, purpose).
func derive(identityKey []byte, purpose string) []byte {
	mac := hmac.New(sha256.New, identityKey)
	mac.Write([]byte(purpose))
	return mac.Sum(nil)
}

// Seal returns the data for a button that viewerUUID will see.
func (k *Keys) Seal(action Action, viewerUUID string, ref Ref) (string, error) {
	plaintext, err := ref.marshal()
	if err != nil {
		return "", err
	}
	tag := k.tag(action, viewerUUID, plaintext)
	sealed := make([]byte, tagSize+plaintextSize)
	copy(sealed, tag)
	cipher.NewCTR(k.enc, tag).XORKeyStream(sealed[tagSize:], plaintext)
	return string(action) + "|" + encoding.EncodeToString(sealed), nil
}

// Open returns what a button refers to, if its data was sealed for viewerUUID
// and action. Otherwise it returns ErrInvalid.
func (k *Keys) Open(action Action, viewerUUID, data string) (Ref, error) {
	encoded, ok := strings.CutPrefix(data, string(action)+"|")
	if !ok {
		return Ref{}, ErrInvalid
	}
	sealed, err := encoding.DecodeString(encoded)
	if err != nil || len(sealed) != tagSize+plaintextSize {
		return Ref{}, ErrInvalid
	}
	tag, ciphertext := sealed[:tagSize], sealed[tagSize:]
	plaintext := make([]byte, plaintextSize)
	cipher.NewCTR(k.enc, tag).XORKeyStream(plaintext, ciphertext)
	if !hmac.Equal(tag, k.tag(action, viewerUUID, plaintext)) {
		return Ref{}, ErrInvalid
	}
	return unmarshalRef(plaintext)
}

func (k *Keys) tag(action Action, viewerUUID string, plaintext []byte) []byte {
	mac := hmac.New(sha256.New, k.mac)
	mac.Write([]byte(action))
	mac.Write([]byte{0})
	mac.Write([]byte(viewerUUID))
	mac.Write([]byte{0})
	mac.Write(plaintext)
	return mac.Sum(nil)[:tagSize]
}

// Role bytes in the plaintext.
const (
	anonymousByte = 1
	knownByte     = 2
)

func (ref Ref) marshal() ([]byte, error) {
	id, err := uuid.Parse(ref.Other)
	if err != nil {
		return nil, fmt.Errorf("button refers to an invalid user UUID: %w", err)
	}
	var role byte
	switch ref.Role {
	case conversation.Anonymous:
		role = anonymousByte
	case conversation.Known:
		role = knownByte
	default:
		return nil, fmt.Errorf("button refers to an invalid role %q", ref.Role)
	}
	if ref.MessageID <= 0 {
		return nil, fmt.Errorf("button refers to an invalid message ID %d", ref.MessageID)
	}
	b := make([]byte, 0, plaintextSize)
	b = append(b, id[:]...)
	b = append(b, role)
	return binary.BigEndian.AppendUint64(b, uint64(ref.MessageID)), nil
}

func unmarshalRef(b []byte) (Ref, error) {
	id, err := uuid.FromBytes(b[:16])
	if err != nil {
		return Ref{}, ErrInvalid
	}
	ref := Ref{Other: id.String(), MessageID: int64(binary.BigEndian.Uint64(b[17:]))}
	switch b[16] {
	case anonymousByte:
		ref.Role = conversation.Anonymous
	case knownByte:
		ref.Role = conversation.Known
	default:
		return Ref{}, ErrInvalid
	}
	return ref, nil
}
