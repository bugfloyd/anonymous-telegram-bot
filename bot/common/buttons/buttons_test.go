package buttons

import (
	"fmt"
	"strings"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

var (
	key     = DeriveKey("123456:TEST-token_value")
	viewer  = "11111111-1111-1111-1111-111111111111"
	other   = "22222222-2222-2222-2222-222222222222"
	another = "33333333-3333-3333-3333-333333333333"
)

func TestSignedRoundTrip(t *testing.T) {
	for _, data := range []string{"o|" + other + "|42", "r|" + other + "|0", "b|" + other + "|7", "ub|" + other + "|2147483647"} {
		signed := Sign(key, viewer, data)
		got, ok := Authenticate(key, viewer, signed, nil)
		if !ok || got != data {
			t.Errorf("Authenticate(Sign(%q)) = %q, %v", data, got, ok)
		}
		if !strings.HasPrefix(signed, strings.SplitN(data, "|", 2)[0]+"|") {
			t.Errorf("signed data %q must keep its prefix for routing", signed)
		}
	}
}

func TestSignedDataFitsTelegramLimit(t *testing.T) {
	longest := Sign(key, viewer, fmt.Sprintf("ub|%s|%d", other, int64(9999999999)))
	if len(longest) > MaxDataLength {
		t.Errorf("signed data is %d bytes, Telegram allows %d: %q", len(longest), MaxDataLength, longest)
	}
}

func TestRejectsDataNotIssuedToThisViewer(t *testing.T) {
	signed := Sign(key, viewer, "o|"+other+"|42")
	if _, ok := Authenticate(key, another, signed, nil); ok {
		t.Error("data signed for one user must not work for another")
	}
	if _, ok := Authenticate(DeriveKey("999:other-token"), viewer, signed, nil); ok {
		t.Error("data signed with another key must be rejected")
	}
}

func TestRejectsTampering(t *testing.T) {
	signed := Sign(key, viewer, "o|"+other+"|42")
	sig := signed[strings.LastIndexByte(signed, '|')+1:]
	tampered := []string{
		"o|" + another + "|42|" + sig, // other user
		"o|" + other + "|43|" + sig,   // other message
		"r|" + other + "|42|" + sig,   // other action
		signed[:len(signed)-1] + flip(signed[len(signed)-1]),
	}
	for _, data := range tampered {
		if _, ok := Authenticate(key, viewer, data, nil); ok {
			t.Errorf("tampered data accepted: %q", data)
		}
	}
}

func TestUnsignedDataOnlyFromTheMessageKeyboard(t *testing.T) {
	legacy := "o|" + other + "|42"
	keyboard := &gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
		{Text: "Open", CallbackData: legacy},
	}}}
	if got, ok := Authenticate(key, viewer, legacy, keyboard); !ok || got != legacy {
		t.Errorf("unsigned data from the message's own button = %q, %v; want accepted", got, ok)
	}
	if _, ok := Authenticate(key, viewer, "o|"+another+"|1", keyboard); ok {
		t.Error("unsigned data that isn't on the message's keyboard must be rejected")
	}
	if _, ok := Authenticate(key, viewer, legacy, nil); ok {
		t.Error("unsigned data must be rejected when the message isn't available")
	}
	if _, ok := Authenticate(key, viewer, "o|"+other+"|42|not-a-sig", keyboard); ok {
		t.Error("data with an invalid signature-like suffix must be rejected")
	}
}

func flip(c byte) string {
	if c == 'A' {
		return "B"
	}
	return "A"
}
