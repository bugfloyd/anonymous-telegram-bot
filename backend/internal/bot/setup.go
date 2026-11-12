package bot

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/incognitochat/incognitochat/backend/internal/telegram/botapi"
)

// DefaultAPI is Telegram's Bot API.
const DefaultAPI = botapi.DefaultAPI

// ErrWebhookURL is returned for a webhook address that isn't a Lambda function URL.
var ErrWebhookURL = errors.New("bot: webhook URL must be an https Lambda function URL")

// Setup points the bot at its webhook and sets its menu button and command.
// The deploy runs it by invoking the function directly, so the token and the
// webhook secret never leave the function.
type Setup struct {
	Token  string
	Secret string
	AppURL string
	API    string       // DefaultAPI unless testing
	Client *http.Client // nil for a client with a timeout
}

// Run applies the bot's settings. It is safe to run on every deploy.
func (s Setup) Run(ctx context.Context, webhookURL string) error {
	if !lambdaURL(webhookURL) {
		return ErrWebhookURL
	}
	en, fa := stringsFor("en"), stringsFor("fa")
	calls := []struct {
		method string
		params map[string]any
	}{
		{"setWebhook", map[string]any{
			"url":             webhookURL,
			"secret_token":    s.Secret,
			"allowed_updates": []string{"message"},
			"max_connections": 10,
		}},
		{"setChatMenuButton", map[string]any{
			"menu_button": map[string]any{"type": "web_app", "text": "Open", "web_app": map[string]string{"url": s.AppURL}},
		}},
		{"setMyCommands", map[string]any{"commands": []map[string]string{{"command": "start", "description": en.command}}}},
		{"setMyCommands", map[string]any{"commands": []map[string]string{{"command": "start", "description": fa.command}}, "language_code": "fa"}},
	}
	for _, c := range calls {
		if err := s.call(ctx, c.method, c.params); err != nil {
			return err
		}
	}
	return nil
}

// call runs one Bot API method.
func (s Setup) call(ctx context.Context, method string, params map[string]any) error {
	return botapi.Client{Token: s.Token, API: s.API, HTTP: s.Client}.Call(ctx, method, params, nil)
}

func lambdaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		strings.Contains(u.Host, ".lambda-url.") && strings.HasSuffix(u.Host, ".on.aws") && (u.Path == "" || u.Path == "/")
}
