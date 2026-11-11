// Package api implements the client's HTTPS API.
//
// Error responses carry a stable code the client acts on:
//
//	bad_request, unsupported_media_type  the request is malformed
//	init_data_rejected                    Telegram's launch data is invalid, too old, or not a registration launch
//	signature_rejected                    a device or account signature is invalid
//	invalid_device_list                   the device list is malformed or doesn't match the request
//	account_exists                        the Telegram user has another account; repeat with replace to end it
//	conflict                              a concurrent change won; try again
//	rate_limited                          too many attempts; try later
//	login_refused                         the device can't log in
//	account_replaced                      the account was replaced by a new registration
//	internal                              something failed on the server
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/session"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/initdata"
)

// Directory is the account store.
type Directory interface {
	ByTelegram(ctx context.Context, telegramHash string) (directory.Account, error)
	Create(ctx context.Context, n directory.NewAccount) error
	Replace(ctx context.Context, old directory.Account, n directory.NewAccount, now time.Time) error
	Device(ctx context.Context, accountID, deviceID string, now time.Time) (directory.Device, error)
}

// Limiter counts uses of a key in fixed windows.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// Server holds what the handlers need.
type Server struct {
	Telegram  initdata.Verifier
	Sessions  *session.Issuer
	Directory Directory
	Nonces    session.NonceStore
	Limits    Limiter
	Pepper    []byte // keys the hash of Telegram user IDs
	Log       *slog.Logger
	Now       func() time.Time
}

// Routes registers the API's routes.
func (s *Server) Routes(r *httpapi.Router) {
	r.Handle(http.MethodPost, "/register", s.register)
	r.Handle(http.MethodPost, "/session", s.session)
}

// tokenResponse is what a successful registration or login returns.
type tokenResponse struct {
	Account string `json:"account"`
	Token   string `json:"token"`
	Expires int64  `json:"expires"` // Unix seconds
}

func (s *Server) issue(route, accountID, deviceID string, now time.Time) httpapi.Response {
	token, expires, err := s.Sessions.Issue(accountID, deviceID, now)
	if err != nil {
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusOK, tokenResponse{Account: accountID, Token: token, Expires: expires.Unix()})
}

// internal logs a server-side failure, without anything about the request.
func (s *Server) internal(route string, err error) httpapi.Response {
	s.Log.Warn("request failed",
		slog.String(logx.KeyRoute, route),
		slog.Int(logx.KeyStatus, http.StatusInternalServerError),
		logx.ErrorKind(err))
	return httpapi.Error(http.StatusInternalServerError, "internal")
}

// bytes64 is a byte string written in JSON as unpadded base64url.
type bytes64 []byte

func (b *bytes64) UnmarshalJSON(data []byte) error {
	var text string
	if err := json.Unmarshal(data, &text); err != nil {
		return err
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil {
		return err
	}
	*b = decoded
	return nil
}
