package main

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/bot"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

func TestHandleRoutesEvents(t *testing.T) {
	b := &bot.Bot{Secret: "s", AppURL: "https://example.github.io/"}
	var setupURL string
	setup := func(_ context.Context, url string) error { setupURL = url; return nil }

	out, err := handle(context.Background(), json.RawMessage(`{"setup":{"webhookUrl":"https://x.lambda-url.us-east-1.on.aws/"}}`), b, setup)
	if err != nil || setupURL != "https://x.lambda-url.us-east-1.on.aws/" {
		t.Fatalf("setup: %v, %v, %q", out, err, setupURL)
	}

	// A function URL request without the secret is a 404, and never a setup.
	setupURL = ""
	out, err = handle(context.Background(), json.RawMessage(`{"rawPath":"/","requestContext":{"http":{"method":"POST"}},"body":"{}"}`), b, setup)
	if err != nil || out.(httpapi.Response).StatusCode != http.StatusNotFound || setupURL != "" {
		t.Fatalf("webhook: %v, %v", out, err)
	}
	out, _ = handle(context.Background(), json.RawMessage(`{"rawPath":"/","requestContext":{"http":{"method":"POST"}},"headers":{"x-telegram-bot-api-secret-token":"s"},"body":"{\"message\":{\"message_id\":1,\"chat\":{\"id\":2,\"type\":\"private\"},\"text\":\"/start\"}}"}`), b, setup)
	if resp := out.(httpapi.Response); resp.StatusCode != http.StatusOK || resp.Body == "" {
		t.Fatalf("start: %+v", resp)
	}

	if _, err := handle(context.Background(), json.RawMessage(`[`), b, setup); err == nil {
		t.Fatal("accepted a broken event")
	}
}
