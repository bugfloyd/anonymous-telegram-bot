package buttons

import (
	"bytes"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/bugfloyd/telegram-incognito-bot/common/conversation"
)

var (
	identityKey = bytes.Repeat([]byte{7}, MinKeyLength)
	viewer      = "11111111-1111-1111-1111-111111111111"
	another     = "33333333-3333-3333-3333-333333333333"
	ref         = Ref{Other: "22222222-2222-2222-2222-222222222222", Role: conversation.Anonymous, MessageID: 42}
	actions     = []Action{OpenMessage, Reply, Block, Unblock}
)

func newKeys(t *testing.T, identityKey []byte) *Keys {
	t.Helper()
	k, err := NewKeys(identityKey)
	if err != nil {
		t.Fatalf("NewKeys: %v", err)
	}
	return k
}

func seal(t *testing.T, k *Keys, action Action, viewer string, ref Ref) string {
	t.Helper()
	data, err := k.Seal(action, viewer, ref)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	return data
}

func TestRoundTrip(t *testing.T) {
	k := newKeys(t, identityKey)
	refs := []Ref{
		ref,
		{Other: ref.Other, Role: conversation.Known, MessageID: 1},
		{Other: ref.Other, Role: conversation.Anonymous, MessageID: math.MaxInt64},
	}
	for _, action := range actions {
		for _, want := range refs {
			data := seal(t, k, action, viewer, want)
			if !strings.HasPrefix(data, string(action)+"|") {
				t.Errorf("data %q must start with %q for routing", data, string(action)+"|")
			}
			got, err := k.Open(action, viewer, data)
			if err != nil || got != want {
				t.Errorf("Open(Seal(%s, %+v)) = %+v, %v", action, want, got, err)
			}
		}
	}
}

func TestFitsTelegramLimit(t *testing.T) {
	k := newKeys(t, identityKey)
	for _, action := range actions {
		if data := seal(t, k, action, viewer, ref); len(data) > MaxDataLength {
			t.Errorf("%s data is %d bytes, Telegram allows %d", action, len(data), MaxDataLength)
		}
	}
}

func TestHidesWhoItRefersTo(t *testing.T) {
	k := newKeys(t, identityKey)
	data := seal(t, k, Reply, viewer, ref)
	for _, part := range []string{ref.Other, strings.ReplaceAll(ref.Other, "-", "")} {
		if strings.Contains(data, part) {
			t.Errorf("data %q shows %q", data, part)
		}
	}
	// Different viewers, and the two roles, must give unrelated data.
	seen := map[string]bool{data: true}
	for _, other := range []string{
		seal(t, k, Reply, another, ref),
		seal(t, k, Reply, viewer, Ref{Other: ref.Other, Role: conversation.Known, MessageID: ref.MessageID}),
	} {
		if seen[other] || other[2:12] == data[2:12] {
			t.Errorf("data isn't unique per viewer and role: %q vs %q", data, other)
		}
		seen[other] = true
	}
}

func TestRejectsDataNotSealedForThisViewerAndAction(t *testing.T) {
	k := newKeys(t, identityKey)
	data := seal(t, k, OpenMessage, viewer, ref)
	if _, err := k.Open(OpenMessage, another, data); !errors.Is(err, ErrInvalid) {
		t.Error("data sealed for one user must not open for another")
	}
	otherKey := bytes.Repeat([]byte{8}, MinKeyLength)
	if _, err := newKeys(t, otherKey).Open(OpenMessage, viewer, data); !errors.Is(err, ErrInvalid) {
		t.Error("data sealed with another identity key must be rejected")
	}
	// Swapping the action, including its routing prefix, must fail.
	swapped := string(Reply) + strings.TrimPrefix(data, string(OpenMessage))
	if _, err := k.Open(Reply, viewer, swapped); !errors.Is(err, ErrInvalid) {
		t.Error("data sealed for OpenMessage must not work as Reply")
	}
	if _, err := k.Open(Reply, viewer, data); !errors.Is(err, ErrInvalid) {
		t.Error("data with another action's prefix must be rejected")
	}
}

func TestRejectsTampering(t *testing.T) {
	k := newKeys(t, identityKey)
	data := seal(t, k, Block, viewer, ref)
	prefix := string(Block) + "|"
	for i := len(prefix); i < len(data); i++ {
		tampered := []byte(data)
		if tampered[i] == 'A' {
			tampered[i] = 'B'
		} else {
			tampered[i] = 'A'
		}
		if _, err := k.Open(Block, viewer, string(tampered)); !errors.Is(err, ErrInvalid) {
			t.Errorf("data changed at byte %d was accepted", i)
		}
	}
	for _, bad := range []string{"", "b", "b|", data + "A", data[:len(data)-1], "b|" + ref.Other + "|42"} {
		if _, err := k.Open(Block, viewer, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("malformed data %q was accepted", bad)
		}
	}
}

func TestSealRejectsInvalidRefs(t *testing.T) {
	k := newKeys(t, identityKey)
	for _, bad := range []Ref{
		{Other: "not-a-uuid", Role: conversation.Anonymous, MessageID: 1},
		{Other: ref.Other, Role: "", MessageID: 1},
		{Other: ref.Other, Role: conversation.Known, MessageID: 0},
	} {
		if _, err := k.Seal(Reply, viewer, bad); err == nil {
			t.Errorf("Seal(%+v) should fail", bad)
		}
	}
}

func TestNewKeysRejectsShortKeys(t *testing.T) {
	if _, err := NewKeys(make([]byte, MinKeyLength-1)); err == nil {
		t.Error("a short identity key must be rejected")
	}
}
