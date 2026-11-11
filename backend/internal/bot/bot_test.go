package bot

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

const secret = "webhook-secret-0123456789"

func request(secretHeader, body string) httpapi.Request {
	var req httpapi.Request
	req.RawPath = "/"
	req.RequestContext.HTTP.Method = http.MethodPost
	req.Headers = map[string]string{"content-type": "application/json"}
	if secretHeader != "" {
		req.Headers["x-telegram-bot-api-secret-token"] = secretHeader
	}
	req.Body = body
	return req
}

func reply(t *testing.T, resp httpapi.Response) map[string]any {
	t.Helper()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %s", resp.StatusCode, resp.Body)
	}
	if resp.Body == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resp.Body), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestHandle(t *testing.T) {
	b := &Bot{Secret: secret, AppURL: "https://example.github.io/"}

	t.Run("start, in Persian", func(t *testing.T) {
		out := reply(t, b.Handle(request(secret, `{"update_id":1,"message":{"message_id":5,"chat":{"id":42,"type":"private"},"from":{"id":42,"language_code":"fa"},"text":"/start"}}`)))
		if out["method"] != "sendMessage" || out["chat_id"] != float64(42) || !strings.Contains(out["text"].(string), "چت ناشناس") {
			t.Fatalf("reply = %v", out)
		}
		button := out["reply_markup"].(map[string]any)["inline_keyboard"].([]any)[0].([]any)[0].(map[string]any)
		if button["web_app"].(map[string]any)["url"] != "https://example.github.io/" {
			t.Fatalf("button = %v", button)
		}
	})

	t.Run("any text, in English", func(t *testing.T) {
		out := reply(t, b.Handle(request(secret, `{"message":{"message_id":6,"chat":{"id":42,"type":"private"},"from":{"language_code":"de"},"text":"hello?"}}`)))
		if !strings.HasPrefix(out["text"].(string), "Incognito Chat") {
			t.Fatalf("reply = %v", out)
		}
	})

	t.Run("write access message is deleted", func(t *testing.T) {
		out := reply(t, b.Handle(request(secret, `{"message":{"message_id":7,"chat":{"id":42,"type":"private"},"write_access_allowed":{"web_app_name":"dev"}}}`)))
		if out["method"] != "deleteMessage" || out["message_id"] != float64(7) || out["chat_id"] != float64(42) {
			t.Fatalf("reply = %v", out)
		}
	})

	t.Run("groups and other updates are ignored", func(t *testing.T) {
		for _, body := range []string{
			`{"message":{"message_id":8,"chat":{"id":-100,"type":"group"},"text":"/start"}}`,
			`{"edited_message":{"message_id":9}}`,
			`{"message":{"message_id":10,"chat":{"id":42,"type":"private"},"sticker":{}}}`,
			`not json`,
		} {
			if out := reply(t, b.Handle(request(secret, body))); out != nil {
				t.Errorf("%s: reply = %v", body, out)
			}
		}
	})

	t.Run("wrong or missing secret looks like nothing is there", func(t *testing.T) {
		for _, header := range []string{"", "wrong", secret + "x"} {
			if resp := b.Handle(request(header, `{}`)); resp.StatusCode != http.StatusNotFound {
				t.Errorf("secret %q: status %d", header, resp.StatusCode)
			}
		}
		req := request(secret, `{}`)
		req.RequestContext.HTTP.Method = http.MethodGet
		if resp := b.Handle(req); resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET: status %d", resp.StatusCode)
		}
		if resp := (&Bot{AppURL: "x"}).Handle(request("", `{}`)); resp.StatusCode != http.StatusNotFound {
			t.Error("a bot without a secret accepted an update")
		}
	})
}

func TestSetup(t *testing.T) {
	var calls []string
	var bodies []map[string]any
	telegram := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer telegram.Close()

	s := Setup{Token: "123:TOKEN", Secret: secret, AppURL: "https://example.github.io/", API: telegram.URL}
	if err := s.Run(context.Background(), "https://abc123.lambda-url.eu-west-1.on.aws/"); err != nil {
		t.Fatal(err)
	}
	want := []string{"/bot123:TOKEN/setWebhook", "/bot123:TOKEN/setChatMenuButton", "/bot123:TOKEN/setMyCommands", "/bot123:TOKEN/setMyCommands"}
	if strings.Join(calls, " ") != strings.Join(want, " ") {
		t.Fatalf("calls = %v", calls)
	}
	if bodies[0]["secret_token"] != secret || bodies[0]["url"] != "https://abc123.lambda-url.eu-west-1.on.aws/" {
		t.Fatalf("setWebhook = %v", bodies[0])
	}

	for _, bad := range []string{"http://abc.lambda-url.eu-west-1.on.aws/", "https://evil.example/", "https://abc.lambda-url.eu-west-1.on.aws/x", "https://u@abc.lambda-url.eu-west-1.on.aws/"} {
		if err := s.Run(context.Background(), bad); err != ErrWebhookURL {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}

func TestSetupErrorsHideTheToken(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"description":"Unauthorized"}`))
	}))
	defer failing.Close()
	for _, api := range []string{failing.URL, "http://127.0.0.1:1"} {
		err := Setup{Token: "123:SECRET-TOKEN", Secret: secret, AppURL: "x", API: api}.Run(context.Background(), "https://abc.lambda-url.eu-west-1.on.aws/")
		if err == nil || strings.Contains(err.Error(), "SECRET-TOKEN") {
			t.Fatalf("err = %v", err)
		}
	}
}
