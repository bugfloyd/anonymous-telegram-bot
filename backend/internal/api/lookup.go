package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

// LookupsPerDay limits account lookups per Telegram user.
const LookupsPerDay = 30

type lookupRequest struct {
	InitData string `json:"initData"`
}

type lookupResponse struct {
	Exists bool `json:"exists"`
}

// lookupAccount tells a device that has no account whether its Telegram user
// has one, which then lives on another device (one device per account, D18).
// The app asks on a plain launch, so a second device (say Telegram Desktop,
// opened from a notice) can say so instead of offering setup, which would
// replace the account. Only plain launches are accepted, as for notices, so
// this never tells the server whose link someone opened. Nothing is stored.
func (s *Server) lookupAccount(ctx context.Context, req httpapi.Request) httpapi.Response {
	const route = "POST /accounts/lookup"
	var in lookupRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if in.InitData == "" || len(in.InitData) > maxInitData {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	launch, err := s.Telegram.Verify(in.InitData, s.Now())
	if err != nil || !PlainLaunch(launch.StartParam) {
		return httpapi.Error(http.StatusUnauthorized, "init_data_rejected")
	}
	telegramHash := directory.TelegramHash(s.Pepper, launch.UserID)
	ok, err := s.Limits.Allow(ctx, "look#"+telegramHash, LookupsPerDay, 24*time.Hour)
	if err != nil {
		return s.internal(route, err)
	}
	if !ok {
		return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
	}
	_, err = s.Directory.ByTelegram(ctx, telegramHash)
	if err != nil && !errors.Is(err, directory.ErrNotFound) {
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusOK, lookupResponse{Exists: err == nil})
}
