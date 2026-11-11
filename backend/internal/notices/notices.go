// Package notices sends the Telegram "New message" notices (design §12).
//
// Notices are generic: "New message" in the user's language with a button
// that opens the app. No labels, counts or names. At most one per account
// every few minutes, and the previous one is deleted when a new one goes out.
//
// The chat ID (the user's Telegram ID) is kept sealed with a key only the
// notify function holds, so neither the api function nor a copy of the
// directory table can read it.
package notices

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"strings"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/botapi"
)

// Interval is the least time between two notices to one account.
const Interval = 3 * time.Minute

const label = "incognitochat/v2/chat-id"

// ErrSealed is returned for a sealed chat ID that doesn't open.
var ErrSealed = errors.New("notices: sealed chat ID doesn't open")

// Sealer seals chat IDs with AES-256-GCM under a key derived from a secret.
type Sealer struct{ aead cipher.AEAD }

// NewSealer derives the sealing key from a secret of at least 32 bytes.
func NewSealer(secret []byte) (*Sealer, error) {
	if len(secret) < 32 {
		return nil, errors.New("notices: chat key too short")
	}
	k := sha256.Sum256(append([]byte(label+":"), secret...))
	block, err := aes.NewCipher(k[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal encrypts a chat ID: nonce ‖ ciphertext.
func (s *Sealer) Seal(chat int64) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return s.aead.Seal(nonce, nonce, binary.BigEndian.AppendUint64(nil, uint64(chat)), []byte(label)), nil
}

// Open decrypts a sealed chat ID.
func (s *Sealer) Open(sealed []byte) (int64, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n {
		return 0, ErrSealed
	}
	pt, err := s.aead.Open(nil, sealed[:n], sealed[n:], []byte(label))
	if err != nil || len(pt) != 8 {
		return 0, ErrSealed
	}
	return int64(binary.BigEndian.Uint64(pt)), nil
}

// Directory is what notices need from the directory table.
type Directory interface {
	Notices(ctx context.Context, accountID string) (directory.NoticeTarget, error)
	NoticeSent(ctx context.Context, accountID string, messageID int64) error
	NoticesOff(ctx context.Context, accountID string) error
}

// Quiet holds the quiet period after a notice.
type Quiet interface {
	Start(ctx context.Context, key string, until time.Time) (bool, error)
}

// Notifier sends notices.
type Notifier struct {
	Directory Directory
	Quiet     Quiet
	Sealer    *Sealer
	Bot       botapi.Client
	AppURL    string
	Now       func() time.Time
}

// Notify sends an account its notice, unless it has notices off, has none
// set up, or had one within Interval.
func (n *Notifier) Notify(ctx context.Context, accountID string) error {
	target, err := n.Directory.Notices(ctx, accountID)
	if errors.Is(err, directory.ErrNotFound) || (err == nil && target.Off) {
		return nil
	}
	if err != nil {
		return err
	}
	ok, err := n.Quiet.Start(ctx, "notice#"+accountID, n.Now().Add(Interval))
	if err != nil || !ok {
		return err
	}
	chat, err := n.Sealer.Open(target.SealedChat)
	if err != nil {
		// Sealed under an earlier key: wait for the app to set it up again.
		return n.Directory.NoticesOff(ctx, accountID)
	}
	if target.LastMessage > 0 {
		// Only the latest notice stays in the chat. It may be gone already.
		_ = n.Bot.Call(ctx, "deleteMessage", map[string]any{"chat_id": chat, "message_id": target.LastMessage}, nil)
	}
	text := textFor(target.Language)
	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	err = n.Bot.Call(ctx, "sendMessage", map[string]any{
		"chat_id":              chat,
		"text":                 text.notice,
		"disable_notification": false,
		"reply_markup": map[string]any{
			"inline_keyboard": [][]map[string]any{{{"text": text.open, "web_app": map[string]string{"url": n.AppURL}}}},
		},
	}, &sent)
	var apiErr *botapi.Error
	if errors.As(err, &apiErr) && apiErr.Forbidden() {
		// Blocked, or never allowed to write: stop until the app turns notices on again.
		return n.Directory.NoticesOff(ctx, accountID)
	}
	if err != nil {
		return err
	}
	return n.Directory.NoticeSent(ctx, accountID, sent.MessageID)
}

// Language returns the notice language for a Telegram language code.
func Language(code string) string {
	if l := strings.ToLower(code); l == "fa" || strings.HasPrefix(l, "fa-") || strings.HasPrefix(l, "fa_") {
		return "fa"
	}
	return "en"
}

type texts struct{ notice, open string }

func textFor(language string) texts {
	if language == "fa" {
		return texts{notice: "پیام جدید", open: "باز کردن"}
	}
	return texts{notice: "New message", open: "Open"}
}
