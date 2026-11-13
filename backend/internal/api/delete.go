package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

// deleteAccount ends the caller's account at once and deletes what the
// server holds for it: its waiting envelopes, links, mailboxes, invitation
// codes, devices and the account itself, and the Telegram item, so the same
// Telegram user can set up again (DELETE /account, signed). Nothing is kept,
// not even a marker. Short-lived records (rate counters, socket records)
// expire on their own within a day.
func (s *Server) deleteAccount(ctx context.Context, _ httpapi.Request, c caller) httpapi.Response {
	const route = "DELETE /account"
	devices, err := s.Directory.Devices(ctx, c.AccountID, s.Now())
	if err != nil {
		return s.internal(route, err)
	}
	// Envelopes first: once the account is gone, the device can't ask again.
	for _, d := range devices {
		if err := s.Inbox.Clear(ctx, c.AccountID, d); err != nil {
			return s.internal(route, err)
		}
	}
	err = s.Directory.Delete(ctx, c.AccountID, c.device.TelegramHash)
	if errors.Is(err, directory.ErrNotFound) {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	if err != nil {
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusOK, map[string]string{})
}
