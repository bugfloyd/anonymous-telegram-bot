// Package botapi calls Telegram's Bot API. Errors never include the request
// URL, which contains the bot token.
package botapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// DefaultAPI is Telegram's Bot API.
const DefaultAPI = "https://api.telegram.org"

// Client calls the Bot API with one bot's token.
type Client struct {
	Token string
	API   string       // DefaultAPI unless testing
	HTTP  *http.Client // nil for a client with a timeout
}

// Error is a failed call: the method, the HTTP status and Telegram's error code.
type Error struct {
	Method string
	Status int
	Code   int
}

func (e *Error) Error() string {
	return fmt.Sprintf("botapi: %s failed (HTTP %d)", e.Method, e.Status)
}

// Forbidden reports whether the bot may not write to the chat (blocked by
// the user, or never allowed to write).
func (e *Error) Forbidden() bool { return e.Code == http.StatusForbidden }

// Call runs a method and decodes its result into result (which may be nil).
func (c Client) Call(ctx context.Context, method string, params any, result any) error {
	body, err := json.Marshal(params)
	if err != nil {
		return err
	}
	api := c.API
	if api == "" {
		api = DefaultAPI
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, api+"/bot"+c.Token+"/"+method, bytes.NewReader(body))
	if err != nil {
		return &Error{Method: method}
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return &Error{Method: method}
	}
	defer resp.Body.Close()
	var out struct {
		OK        bool            `json:"ok"`
		ErrorCode int             `json:"error_code"`
		Result    json.RawMessage `json:"result"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || !out.OK {
		return &Error{Method: method, Status: resp.StatusCode, Code: out.ErrorCode}
	}
	if result != nil {
		if err := json.Unmarshal(out.Result, result); err != nil {
			return &Error{Method: method, Status: resp.StatusCode}
		}
	}
	return nil
}
