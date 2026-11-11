package envelope

import (
	"bytes"
	"encoding/hex"
	"os"
	"strings"
	"testing"
)

func TestClientEnvelopes(t *testing.T) {
	// Sealed by the client (web/src/protocol/envelope.ts): one wrap with a
	// 512-byte body, two wraps with a 2048-byte body.
	raw, err := os.ReadFile("testdata/client-envelopes.hex")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Fields(string(raw))
	for i, want := range []int{1, 2} {
		env, err := hex.DecodeString(lines[i])
		if err != nil {
			t.Fatal(err)
		}
		if got, err := Check(env); err != nil || got != want {
			t.Fatalf("envelope %d: %d wraps, %v", i, got, err)
		}
	}
}

func TestCheckRejects(t *testing.T) {
	valid := func(wraps, padded int) []byte {
		env := []byte{Suite, byte(wraps)}
		return append(env, make([]byte, wraps*WrapLen+nonceLen+padded+tagLen)...)
	}
	if _, err := Check(valid(MaxWraps, 16384)); err != nil {
		t.Fatalf("largest envelope refused: %v", err)
	}
	if MaxLen != len(valid(MaxWraps, 16384)) {
		t.Fatalf("MaxLen = %d", MaxLen)
	}
	for name, env := range map[string][]byte{
		"empty":         nil,
		"other suite":   append([]byte{2}, valid(1, 512)[1:]...),
		"no wraps":      valid(0, 512),
		"too many":      valid(MaxWraps+1, 512),
		"unpadded body": valid(1, 500),
		"extra byte":    append(valid(1, 512), 0),
		"too long":      bytes.Repeat([]byte{1}, MaxLen+1),
	} {
		if _, err := Check(env); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
