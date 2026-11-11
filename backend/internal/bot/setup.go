package bot

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultAPI is Telegram's Bot API.
const DefaultAPI = "https://api.telegram.org"

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

// call runs one Bot API method. Errors never include the request URL, which
// contains the token.
func (s Setup) call(ctx context.Context, method string, params map[string]any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	api := s.API
	if api == "" {
		api = DefaultAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/bot"+s.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("bot: %s: bad request", method)
	}
	req.Header.Set("Content-Type", "application/json")
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("bot: %s: request failed", method)
	}
	defer resp.Body.Close()
	var out struct {
		OK bool `json:"ok"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || !out.OK {
		return fmt.Errorf("bot: %s failed (HTTP %d)", method, resp.StatusCode)
	}
	return nil
}

func lambdaURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.User == nil && u.RawQuery == "" && u.Fragment == "" &&
		strings.Contains(u.Host, ".lambda-url.") && strings.HasSuffix(u.Host, ".on.aws") && (u.Path == "" || u.Path == "/")
}
