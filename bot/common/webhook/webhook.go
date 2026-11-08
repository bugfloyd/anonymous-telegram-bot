// Package webhook authenticates incoming Telegram webhook requests.
//
// Telegram sends the secret_token given to setWebhook in the
// X-Telegram-Bot-Api-Secret-Token header of every update. The secret is
// derived from the bot token, so there is no extra secret to store or rotate:
// rotating the bot token rotates the webhook secret too. scripts/set-webhook.sh
// derives the same value when it registers the webhook.
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"
)

// SecretHeader is the header Telegram uses to send the webhook secret token.
const SecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// derivationLabel must stay in sync with scripts/set-webhook.sh.
const derivationLabel = "telegram-webhook-secret"

// DeriveSecret returns the webhook secret token for a bot token:
// hex(HMAC-SHA256(key=botToken, message=derivationLabel)). The result has
// 64 characters, all within Telegram's allowed set [A-Za-z0-9_-].
func DeriveSecret(botToken string) string {
	mac := hmac.New(sha256.New, []byte(botToken))
	mac.Write([]byte(derivationLabel))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidRequest reports whether the headers carry the expected secret token.
// The header lookup ignores case, because API Gateway may lowercase header names.
func ValidRequest(headers map[string]string, expectedSecret string) bool {
	if expectedSecret == "" {
		return false
	}
	for name, value := range headers {
		if strings.EqualFold(name, SecretHeader) {
			return subtle.ConstantTimeCompare([]byte(value), []byte(expectedSecret)) == 1
		}
	}
	return false
}
