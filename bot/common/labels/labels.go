// Package labels gives the people a user talks to short labels, such as
// "🦊 K3F9", so the user can tell them apart without learning who they are.
//
// A label is HMAC-SHA256(labelKey, otherRole ‖ viewerUUID ‖ otherUUID), shown
// as one of 64 emoji and 4 characters of Crockford base32 (about 67 million
// labels). So:
//   - the same person always has the same label in the same conversation,
//   - every viewer sees a different label for them, so viewers can't compare,
//   - and their two possible conversations with the same viewer (anonymous to
//     them, or known to them; see conversation.Role) get unrelated labels, so a
//     label never links someone's anonymous messages to their public link.
//
// Nothing is stored. The key is derived from the bot's identity key (see the
// secrets package). The emoji list and the alphabet are frozen: changing or
// reordering either changes everyone's labels.
package labels

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/bugfloyd/telegram-incognito-bot/common/conversation"
)

// emoji: single code points shown as emoji by default (no variation
// selectors, skin tones or flags), easy to tell apart, and none that reads as
// an insult in English or Persian.
var emoji = [64]string{
	"🦊", "🐼", "🐙", "🦉", "🐢", "🦄", "🐝", "🦖", "🦋", "🐧", "🦁", "🦌", "🐨", "🐸", "🦀", "🐳",
	"🦒", "🦓", "🦔", "🦦", "🦩", "🦭", "🐞", "🦜", "🦚", "🦘", "🐘", "🐪", "🦢", "🐠", "🦈", "🐉",
	"🌵", "🌻", "🍄", "🍉", "🍋", "🍒", "🍍", "🥥", "🥑", "🍩", "🧁", "🎈", "🚀", "⚓", "🎲", "🔔",
	"💎", "🌙", "⭐", "🌈", "🍀", "🪁", "🧩", "🌊", "🔥", "⚡", "🎸", "🎨", "🧭", "🪐", "🍁", "🌸",
}

// alphabet is Crockford's base32: no I, L, O or U, so codes are easy to read.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const minKeyLength = 32

// Key derives labels.
type Key []byte

// NewKey derives the label key from the identity key.
func NewKey(identityKey []byte) (Key, error) {
	if len(identityKey) < minKeyLength {
		return nil, fmt.Errorf("identity key must be at least %d bytes, got %d", minKeyLength, len(identityKey))
	}
	mac := hmac.New(sha256.New, identityKey)
	mac.Write([]byte("sender-label-v1"))
	return mac.Sum(nil), nil
}

// For returns the label viewerUUID sees for otherUUID, whose role in their
// conversation is otherRole.
func (k Key) For(viewerUUID, otherUUID string, otherRole conversation.Role) string {
	mac := hmac.New(sha256.New, k)
	mac.Write([]byte(otherRole))
	mac.Write([]byte{0})
	mac.Write([]byte(viewerUUID))
	mac.Write([]byte{0})
	mac.Write([]byte(otherUUID))
	h := mac.Sum(nil)

	var b strings.Builder
	b.WriteString(emoji[h[0]%64])
	b.WriteByte(' ')
	code := uint32(h[1])<<16 | uint32(h[2])<<8 | uint32(h[3])
	for shift := 15; shift >= 0; shift -= 5 {
		b.WriteByte(alphabet[code>>shift&31])
	}
	return b.String()
}

// Embed marks a label as left-to-right, so its emoji and code keep their
// order inside right-to-left (Persian) text. The marks are invisible.
func Embed(label string) string {
	return "‎" + label + "‎"
}
