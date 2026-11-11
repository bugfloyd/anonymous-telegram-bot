package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/session"
)

type sessionRequest struct {
	Account   string  `json:"account"`
	Device    string  `json:"device"`
	Timestamp int64   `json:"timestamp"`
	Nonce     string  `json:"nonce"`
	Signature bytes64 `json:"signature"`
}

// session logs a device in (design §7). A device of a replaced account learns
// so only after its signature checks out.
func (s *Server) session(ctx context.Context, req httpapi.Request) httpapi.Response {
	const route = "POST /session"
	var in sessionRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if !ids.Valid(in.Account) || !ids.Valid(in.Device) {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	now := s.Now()

	device, err := s.Directory.Device(ctx, in.Account, in.Device, now)
	if errors.Is(err, directory.ErrNotFound) {
		return httpapi.Error(http.StatusUnauthorized, "login_refused")
	}
	if err != nil {
		return s.internal(route, err)
	}
	key, err := devicesig.ParsePublicKey(device.Key)
	if err != nil {
		return s.internal(route, err)
	}

	login := session.Login{
		AccountID: in.Account,
		DeviceID:  in.Device,
		Timestamp: in.Timestamp,
		Nonce:     in.Nonce,
		Signature: in.Signature,
	}
	err = session.Check(ctx, login, key, s.Nonces, now)
	if errors.Is(err, session.ErrLogin) {
		return httpapi.Error(http.StatusUnauthorized, "login_refused")
	}
	if err != nil {
		return s.internal(route, err)
	}
	if device.Replaced {
		return httpapi.Error(http.StatusGone, "account_replaced")
	}
	return s.issue(route, in.Account, in.Device, now)
}
