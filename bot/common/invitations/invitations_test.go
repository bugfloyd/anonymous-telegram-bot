package invitations

import (
	"strings"
	"testing"
)

func TestParseInviteOnly(t *testing.T) {
	for value, want := range map[string]bool{
		"": true, "true": true, "TRUE": true, "1": true, "anything": true,
		"false": false, "False": false, " false ": false,
	} {
		if got := parseInviteOnly(value); got != want {
			t.Errorf("parseInviteOnly(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestParseAdminIDs(t *testing.T) {
	ids := parseAdminIDs(" 123, 456 ,,abc,-7, 0")
	if len(ids) != 2 || !ids[123] || !ids[456] {
		t.Errorf("parseAdminIDs = %v, want {123, 456}", ids)
	}
	if len(parseAdminIDs("")) != 0 {
		t.Error("empty value should give no admins")
	}
}

func TestCanOwnLink(t *testing.T) {
	t.Setenv("INVITE_ONLY", "")
	if CanOwnLink(false, 1) {
		t.Error("unregistered non-admin must not own a link while invite-only")
	}
	if !CanOwnLink(true, 1) {
		t.Error("registered users can own a link")
	}
	t.Setenv("INVITE_ONLY", "false")
	if !CanOwnLink(false, 1) {
		t.Error("everyone can own a link when the bot is open")
	}
}

func TestRemaining(t *testing.T) {
	tests := []struct{ allowance, granted, allocated, want int }{
		{0, 0, 0, 0},
		{3, 0, 0, 3},
		{3, 2, 1, 4},
		{3, 0, 3, 0},
		{2, 0, 5, 0}, // allowance lowered below what was already given out
	}
	for _, tt := range tests {
		if got := Remaining(tt.allowance, tt.granted, tt.allocated); got != tt.want {
			t.Errorf("Remaining(%d, %d, %d) = %d, want %d", tt.allowance, tt.granted, tt.allocated, got, tt.want)
		}
	}
}

func TestNewCode(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		code, err := NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != 9 || code[4] != '-' {
			t.Fatalf("code %q is not in the form xxxx-xxxx", code)
		}
		if normalized, ok := NormalizeCode(code); !ok || normalized != code {
			t.Fatalf("code %q doesn't round-trip: %q, %v", code, normalized, ok)
		}
		if strings.ContainsAny(code, "01ilo") {
			t.Fatalf("code %q contains an ambiguous character", code)
		}
		seen[code] = true
	}
	if len(seen) < 495 {
		t.Errorf("only %d distinct codes out of 500", len(seen))
	}
}

func TestNormalizeCode(t *testing.T) {
	valid := map[string]string{
		"abcd-efgh":     "abcd-efgh",
		"ABCD-EFGH":     "abcd-efgh",
		"  abcd efgh ":  "abcd-efgh",
		"abcdefgh":      "abcd-efgh",
		"inv-abcd-efgh": "abcd-efgh",
		"https://t.me/somebot?start=inv-abcd-efgh": "abcd-efgh",
	}
	for input, want := range valid {
		if got, ok := NormalizeCode(input); !ok || got != want {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"", "abc-def", "abcd-efgh-jk", "abcd-efg0", "abcd-efgl", "hello there", "https://t.me/somebot?start=Xy3"} {
		if got, ok := NormalizeCode(input); ok {
			t.Errorf("NormalizeCode(%q) = %q, want invalid", input, got)
		}
	}
}

func TestLinkPayloadRoundTrip(t *testing.T) {
	code, _ := NewCode()
	payload := LinkPayload(code)
	if len(payload) > 64 {
		t.Errorf("payload %q exceeds Telegram's 64-character limit", payload)
	}
	if got, ok := CodeFromPayload(payload); !ok || got != code {
		t.Errorf("CodeFromPayload(%q) = %q, %v", payload, got, ok)
	}
	// User-link payloads (sqids, or _username) are not invitations.
	for _, p := range []string{"Xy3Ab9", "_alice"} {
		if _, ok := CodeFromPayload(p); ok {
			t.Errorf("CodeFromPayload(%q) should not be an invitation", p)
		}
	}
}
