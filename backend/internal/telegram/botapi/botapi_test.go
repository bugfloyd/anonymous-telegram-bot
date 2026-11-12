package botapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCall(t *testing.T) {
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":42}}`))
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`))
		}
	}))
	defer telegram.Close()
	c := Client{Token: "123:SECRET-TOKEN", API: telegram.URL}

	var sent struct {
		MessageID int64 `json:"message_id"`
	}
	if err := c.Call(context.Background(), "sendMessage", map[string]any{"chat_id": 1}, &sent); err != nil || sent.MessageID != 42 {
		t.Fatalf("sendMessage = %+v, %v", sent, err)
	}
	err := c.Call(context.Background(), "deleteMessage", map[string]any{}, nil)
	var apiErr *Error
	if !errors.As(err, &apiErr) || !apiErr.Forbidden() || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("err = %v", err)
	}
	// A transport failure doesn't leak the URL either.
	err = Client{Token: "123:SECRET-TOKEN", API: "http://127.0.0.1:1"}.Call(context.Background(), "getMe", nil, nil)
	if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
		t.Fatalf("err = %v", err)
	}
}
