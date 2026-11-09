package labels

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/bugfloyd/telegram-incognito-bot/common/conversation"
)

var (
	identityKey = bytes.Repeat([]byte{7}, minKeyLength)
	alice       = "11111111-1111-1111-1111-111111111111"
	bob         = "22222222-2222-2222-2222-222222222222"
	carol       = "33333333-3333-3333-3333-333333333333"
)

func newKey(t *testing.T) Key {
	t.Helper()
	k, err := NewKey(identityKey)
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	return k
}

// TestFrozenLabels locks in known labels: if this fails, a change to the
// derivation, the emoji list or the alphabet would change everyone's labels.
func TestFrozenLabels(t *testing.T) {
	k := newKey(t)
	for _, tc := range []struct {
		viewer, other string
		role          conversation.Role
		want          string
	}{
		{alice, bob, conversation.Anonymous, "🦁 JNGX"},
		{alice, bob, conversation.Known, "🌵 AMHF"},
		{bob, alice, conversation.Anonymous, "🐞 QKAF"},
		{carol, bob, conversation.Anonymous, "🧁 MG0K"},
	} {
		if got := k.For(tc.viewer, tc.other, tc.role); got != tc.want {
			t.Errorf("For(%s, %s, %s) = %q, want %q", tc.viewer[:1], tc.other[:1], tc.role, got, tc.want)
		}
	}
}

func TestLabelsDontLinkPeopleOrConversations(t *testing.T) {
	k := newKey(t)
	labels := map[string]string{
		"alice sees bob, anonymous": k.For(alice, bob, conversation.Anonymous),
		"alice sees bob, known":     k.For(alice, bob, conversation.Known),
		"bob sees alice, anonymous": k.For(bob, alice, conversation.Anonymous),
		"carol sees bob, anonymous": k.For(carol, bob, conversation.Anonymous),
	}
	seen := map[string]string{}
	for name, label := range labels {
		if other, ok := seen[label]; ok {
			t.Errorf("%q and %q got the same label %q", name, other, label)
		}
		seen[label] = name
	}
	if k.For(alice, bob, conversation.Anonymous) != labels["alice sees bob, anonymous"] {
		t.Error("labels must be stable")
	}
	other, err := NewKey(bytes.Repeat([]byte{8}, minKeyLength))
	if err != nil {
		t.Fatalf("NewKey: %v", err)
	}
	if other.For(alice, bob, conversation.Anonymous) == labels["alice sees bob, anonymous"] {
		t.Error("another identity key must give other labels")
	}
}

func TestLabelFormat(t *testing.T) {
	k := newKey(t)
	label := k.For(alice, bob, conversation.Anonymous)
	symbol, code, ok := strings.Cut(label, " ")
	if !ok || !contains(emoji[:], symbol) || len(code) != 4 || strings.Trim(code, alphabet) != "" {
		t.Errorf("label %q isn't an emoji, a space and 4 base32 characters", label)
	}
}

func TestEmojiList(t *testing.T) {
	seen := map[string]bool{}
	for i, e := range emoji {
		r, _ := utf8.DecodeRuneInString(e)
		if utf8.RuneCountInString(e) != 1 || r == utf8.RuneError {
			t.Errorf("emoji %d (%q) must be a single code point", i, e)
		}
		if seen[e] {
			t.Errorf("emoji %d (%q) is listed twice", i, e)
		}
		seen[e] = true
	}
	if len(alphabet) != 32 {
		t.Errorf("alphabet has %d characters, want 32", len(alphabet))
	}
}

func TestEmbed(t *testing.T) {
	if got := Embed("🦊 K3F9"); got != "‎🦊 K3F9‎" {
		t.Errorf("Embed = %q", got)
	}
}

func TestNewKeyRejectsShortKeys(t *testing.T) {
	if _, err := NewKey(make([]byte, minKeyLength-1)); err == nil {
		t.Error("a short identity key must be rejected")
	}
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}
