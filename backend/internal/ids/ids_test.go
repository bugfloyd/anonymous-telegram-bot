package ids

import "testing"

func TestNew(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := New()
		if !Valid(id) {
			t.Fatalf("New() = %q is not valid", id)
		}
		if seen[id] {
			t.Fatalf("New() repeated %q", id)
		}
		seen[id] = true
	}
}

func TestRoundTrip(t *testing.T) {
	b := [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 255}
	id := Encode(b)
	got, err := Decode(id)
	if err != nil || got != b {
		t.Fatalf("Decode(Encode(b)) = %v, %v", got, err)
	}
}

func TestValidRejects(t *testing.T) {
	for _, id := range []string{
		"",
		"AAAAAAAAAAAAAAAAAAAAA",   // 21 characters
		"AAAAAAAAAAAAAAAAAAAAAAA", // 23 characters
		"AAAAAAAAAAAAAAAAAAAA+A",  // standard base64, not base64url
		"AAAAAAAAAAAAAAAAAAAA/A",
		"AAAAAAAAAAAAAAAAAAAA=A",
		"AAAAAAAAAAAAAAAAAAAAAB", // non-zero padding bits: a second spelling of the same bytes
	} {
		if Valid(id) {
			t.Errorf("Valid(%q) = true", id)
		}
	}
}
