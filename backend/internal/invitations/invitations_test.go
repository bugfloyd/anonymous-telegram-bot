package invitations

import (
	"regexp"
	"testing"
)

func TestNewCode(t *testing.T) {
	seen := map[string]bool{}
	pattern := regexp.MustCompile(`^[a-z2-9]{4}-[a-z2-9]{4}$`)
	for range 200 {
		code, err := NewCode()
		if err != nil || !pattern.MatchString(code) {
			t.Fatalf("NewCode = %q, %v", code, err)
		}
		if got, ok := Normalize(code); !ok || got != code {
			t.Fatalf("Normalize(%q) = %q, %v", code, got, ok)
		}
		seen[code] = true
	}
	if len(seen) < 199 {
		t.Fatalf("only %d distinct codes in 200", len(seen))
	}
}

func TestNormalize(t *testing.T) {
	valid := map[string]string{
		"abcd-efgh":    "abcd-efgh",
		"ABCD-EFGH":    "abcd-efgh",
		"  abcd efgh ": "abcd-efgh",
		"abcdefgh":     "abcd-efgh",
		"invabcdefgh":  "abcd-efgh",
		"https://t.me/some_bot/app?startapp=invabcdefgh": "abcd-efgh",
	}
	for input, want := range valid {
		if got, ok := Normalize(input); !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	for _, input := range []string{"", "abc-def", "abcd-efgh-jk", "abcd-efg0", "abcd-efgl", "hello there", "https://t.me/some_bot/app?startapp=1abc"} {
		if got, ok := Normalize(input); ok {
			t.Errorf("Normalize(%q) = %q, want invalid", input, got)
		}
	}
}

func TestPayload(t *testing.T) {
	if p := Payload("abcd-efgh"); p != "invabcdefgh" {
		t.Fatalf("Payload = %q", p)
	}
}

func TestRemaining(t *testing.T) {
	if Remaining(5, 2, 3) != 4 || Remaining(1, 0, 3) != 0 {
		t.Fatal("Remaining is wrong")
	}
}
