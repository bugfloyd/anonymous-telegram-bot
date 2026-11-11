package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/notices"
)

func TestHandle(t *testing.T) {
	sealer, _ := notices.NewSealer(bytes.Repeat([]byte("k"), 32))
	var notified string
	notify := func(_ context.Context, account string) error { notified = account; return nil }

	out, err := handle(context.Background(), json.RawMessage(`{"seal":{"chat":4242}}`), sealer, notify)
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := base64.RawURLEncoding.DecodeString(out.(notices.SealResult).Sealed)
	if chat, err := sealer.Open(sealed); err != nil || chat != 4242 {
		t.Fatalf("sealed = %d, %v", chat, err)
	}
	account := ids.New()
	if _, err := handle(context.Background(), json.RawMessage(`{"notify":{"account":"`+account+`"}}`), sealer, notify); err != nil || notified != account {
		t.Fatalf("notify: %v, %q", err, notified)
	}
	notified = "untouched"
	if _, err := handle(context.Background(), json.RawMessage(`{"notify":{"account":""}}`), sealer, notify); err != nil || notified != "untouched" {
		t.Fatalf("empty account: %v, %q", err, notified)
	}
	for _, bad := range []string{`{}`, `[`, `{"seal":{"chat":0}}`, `{"notify":{"account":"x"}}`} {
		if _, err := handle(context.Background(), json.RawMessage(bad), sealer, notify); err == nil {
			t.Errorf("%s: accepted", bad)
		}
	}
}
