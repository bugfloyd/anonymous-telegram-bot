// Package bot is the Telegram side of the app (design §15). The bot only
// opens the app and tidies up: it answers messages with a button that opens
// the Mini App, and deletes the service message Telegram posts when a user
// lets the bot write to them. It never sees what people write to each other,
// and keeps nothing.
//
// Replies go back in the webhook response itself (Telegram runs a method
// returned there), so handling an update makes no outgoing request and needs
// no token. The token is only used by Setup, which the deploy runs.
package bot

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

// SecretHeader carries the webhook secret on every update from Telegram.
const SecretHeader = "X-Telegram-Bot-Api-Secret-Token"

// Bot handles webhook updates.
type Bot struct {
	// Secret is the webhook secret Telegram sends with each update.
	Secret string
	// AppURL is the Mini App's address, opened by the reply's button.
	AppURL string
}

type update struct {
	Message *struct {
		MessageID int64 `json:"message_id"`
		Chat      struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"chat"`
		From *struct {
			LanguageCode string `json:"language_code"`
		} `json:"from"`
		Text               string          `json:"text"`
		WriteAccessAllowed json.RawMessage `json:"write_access_allowed"`
	} `json:"message"`
}

// Handle answers one webhook request. Anything without the right secret gets
// the same 404 as an unknown address.
func (b *Bot) Handle(req httpapi.Request) httpapi.Response {
	if !strings.EqualFold(req.RequestContext.HTTP.Method, http.MethodPost) || req.RawPath != "/" && req.RawPath != "" ||
		b.Secret == "" || subtle.ConstantTimeCompare([]byte(httpapi.Header(req, SecretHeader)), []byte(b.Secret)) != 1 {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	body, err := httpapi.Body(req)
	if err != nil {
		return ok(nil)
	}
	var u update
	// Telegram adds fields over time: unknown ones are fine here.
	if json.Unmarshal(body, &u) != nil || u.Message == nil || u.Message.Chat.Type != "private" {
		return ok(nil)
	}
	m := u.Message
	switch {
	case len(m.WriteAccessAllowed) > 0:
		// "You allowed this bot to message you": shown when the app asks for
		// permission to send notices. Nothing to keep.
		return ok(map[string]any{"method": "deleteMessage", "chat_id": m.Chat.ID, "message_id": m.MessageID})
	case m.Text != "":
		lang := ""
		if m.From != nil {
			lang = m.From.LanguageCode
		}
		s := stringsFor(lang)
		return ok(map[string]any{
			"method":  "sendMessage",
			"chat_id": m.Chat.ID,
			"text":    s.welcome,
			"reply_markup": map[string]any{
				"inline_keyboard": [][]map[string]any{{
					{"text": s.open, "web_app": map[string]string{"url": b.AppURL}},
				}},
			},
		})
	}
	return ok(nil)
}

// ok answers Telegram with 200, optionally with a method for it to run.
func ok(method map[string]any) httpapi.Response {
	if method == nil {
		return httpapi.Response{StatusCode: http.StatusOK}
	}
	return httpapi.JSON(http.StatusOK, method)
}

type texts struct {
	welcome string
	open    string
	command string
}

func stringsFor(languageCode string) texts {
	if l := strings.ToLower(languageCode); l == "fa" || strings.HasPrefix(l, "fa-") {
		return texts{
			welcome: "با چت ناشناس دیگران می‌توانند ناشناس و با رمزنگاری سرتاسری به شما پیام بدهند. برای راه‌اندازی یا خواندن پیام‌ها، برنامه را باز کنید.",
			open:    "باز کردن چت ناشناس",
			command: "باز کردن چت ناشناس",
		}
	}
	return texts{
		welcome: "Incognito Chat lets people message you anonymously, end-to-end encrypted. Open the app to set up or to read your messages.",
		open:    "Open Incognito Chat",
		command: "Open Incognito Chat",
	}
}
