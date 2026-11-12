package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/notices"
)

// NoticeSetupsPerDay limits how often an account sets up notices.
const NoticeSetupsPerDay = 10

// Notices reaches the notify function, which alone holds the bot token and
// the key that seals chat IDs (design §12).
type Notices interface {
	// Seal turns a Telegram chat ID into what the directory stores.
	Seal(ctx context.Context, chat int64) ([]byte, error)
	// Notify asks for an account's notice without waiting for it. An empty
	// account does nothing, at the same cost.
	Notify(ctx context.Context, account string) error
}

type noticesRequest struct {
	InitData string `json:"initData"`
}

// setNotices turns on Telegram notices for the caller's account, or updates
// them. The app sends its Telegram launch data, which carries the chat to
// notify; only launches without a link or invitation in them are accepted, so
// this never tells the server whose link someone opened. Only a sealed chat ID
// and the notice language are kept.
func (s *Server) setNotices(ctx context.Context, req httpapi.Request, c caller) httpapi.Response {
	const route = "POST /notices"
	var in noticesRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if in.InitData == "" || len(in.InitData) > maxInitData {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	if s.Notices == nil {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	launch, err := s.Telegram.Verify(in.InitData, s.Now())
	if err != nil || !PlainLaunch(launch.StartParam) ||
		directory.TelegramHash(s.Pepper, launch.UserID) != c.device.TelegramHash {
		return httpapi.Error(http.StatusUnauthorized, "init_data_rejected")
	}
	ok, err := s.Limits.Allow(ctx, "ntc#"+c.AccountID, NoticeSetupsPerDay, 24*time.Hour)
	if err != nil {
		return s.internal(route, err)
	}
	if !ok {
		return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
	}
	// In a private chat with the bot, the chat ID is the user ID.
	sealed, err := s.Notices.Seal(ctx, launch.UserID)
	if err != nil {
		return s.internal(route, err)
	}
	err = s.Directory.SetNotices(ctx, c.AccountID, sealed, notices.Language(launch.LanguageCode))
	if errors.Is(err, directory.ErrNotFound) {
		return httpapi.Error(http.StatusUnauthorized, "session_expired")
	}
	if err != nil {
		return s.internal(route, err)
	}
	return accepted()
}

// PlainLaunch reports whether launch data with this start_param may be sent
// to the server outside registration (notices, account lookups): a plain
// launch or a registration, never one that names a link or an invitation,
// which would tell the server whose link someone opened.
func PlainLaunch(startParam string) bool {
	return startParam == "" || strings.HasPrefix(startParam, RegistrationPrefix)
}

// push tells an account's open apps about a delivery. A failure only means
// the app finds it at its next poll, so it's logged, not returned.
func (s *Server) push(ctx context.Context, account string) bool {
	if s.Push == nil {
		return false
	}
	ok, err := s.Push.Push(ctx, account)
	if err != nil {
		s.Log.Warn("push failed", slog.String(logx.KeyRoute, "POST /send"), logx.ErrorKind(err))
	}
	return ok
}

// notify asks for a notice after a delivery. A failure only costs the
// notice, so it's logged, not returned.
func (s *Server) notify(ctx context.Context, account string) {
	if s.Notices == nil {
		return
	}
	if err := s.Notices.Notify(ctx, account); err != nil {
		s.Log.Warn("notice request failed", slog.String(logx.KeyRoute, "POST /send"), logx.ErrorKind(err))
	}
}
