package webhook

import "testing"

// The expected value was computed independently with Python's hmac module,
// the same way scripts/set-webhook.sh does it. If this test fails, the bot
// and the webhook registration script no longer agree on the secret.
func TestDeriveSecretMatchesScript(t *testing.T) {
	const token = "123456:TEST-token_value"
	const want = "32920171ff97d75e988c29980b203e539583671396cd9b2b94df9a2a51cdafb0"
	if got := DeriveSecret(token); got != want {
		t.Fatalf("DeriveSecret() = %s, want %s", got, want)
	}
}

func TestValidRequest(t *testing.T) {
	secret := DeriveSecret("123456:TEST-token_value")
	tests := []struct {
		name     string
		headers  map[string]string
		expected string
		want     bool
	}{
		{"canonical header", map[string]string{"X-Telegram-Bot-Api-Secret-Token": secret}, secret, true},
		{"lowercase header", map[string]string{"x-telegram-bot-api-secret-token": secret}, secret, true},
		{"wrong secret", map[string]string{"X-Telegram-Bot-Api-Secret-Token": "nope"}, secret, false},
		{"missing header", map[string]string{"Content-Type": "application/json"}, secret, false},
		{"nil headers", nil, secret, false},
		{"empty expected secret", map[string]string{"X-Telegram-Bot-Api-Secret-Token": ""}, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ValidRequest(tt.headers, tt.expected); got != tt.want {
				t.Errorf("ValidRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}
