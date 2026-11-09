// Package buttons signs the data of buttons that refer to another user, so the
// bot only acts on button data it issued to the person pressing the button.
//
// Signed data is "<data>|<sig>", where sig is a truncated
// HMAC-SHA256(key, viewerUUID ‖ data) and the viewer is the user the button
// was shown to. The key is derived from the bot token, so nothing is stored.
package buttons

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// MaxDataLength is Telegram's limit for callback data.
const MaxDataLength = 64

const (
	keyLabel = "callback-mac-v1"
	sigBytes = 8 // 64-bit signature, base64url-encoded to 11 characters
)

var encoding = base64.RawURLEncoding

// DeriveKey derives the signing key from the bot token.
func DeriveKey(botToken string) []byte {
	mac := hmac.New(sha256.New, []byte(botToken))
	mac.Write([]byte(keyLabel))
	return mac.Sum(nil)
}

func signature(key []byte, viewerUUID, data string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(viewerUUID))
	mac.Write([]byte{0})
	mac.Write([]byte(data))
	return encoding.EncodeToString(mac.Sum(nil)[:sigBytes])
}

// Sign returns data with a signature for the user who will see the button.
func Sign(key []byte, viewerUUID, data string) string {
	return data + "|" + signature(key, viewerUUID, data)
}

// Authenticate checks button data pressed by viewerUUID and returns the data
// without its signature.
//
// Signed data must carry a valid signature for this viewer. Unsigned data
// (buttons created before signing existed) is only accepted if it exactly
// matches a button in the pressed message's keyboard, which Telegram supplies;
// markup is nil when the message isn't available to the bot.
func Authenticate(key []byte, viewerUUID, data string, markup *gotgbot.InlineKeyboardMarkup) (string, bool) {
	if i := strings.LastIndexByte(data, '|'); i >= 0 {
		payload, sig := data[:i], data[i+1:]
		if raw, err := encoding.DecodeString(sig); err == nil && len(raw) == sigBytes {
			return payload, hmac.Equal([]byte(sig), []byte(signature(key, viewerUUID, payload)))
		}
	}
	return data, InKeyboard(markup, data)
}

// InKeyboard reports whether data is the callback data of a button in markup.
func InKeyboard(markup *gotgbot.InlineKeyboardMarkup, data string) bool {
	if markup == nil || data == "" {
		return false
	}
	for _, row := range markup.InlineKeyboard {
		for _, button := range row {
			if button.CallbackData == data {
				return true
			}
		}
	}
	return false
}
