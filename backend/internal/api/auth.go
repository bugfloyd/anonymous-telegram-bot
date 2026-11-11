package api

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/incognitochat/incognitochat/backend/internal/devicesig"
	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/session"
)

// SignatureHeader carries a device's signature on a sensitive request:
// "<timestamp>.<nonce>.<signature>", with the signature in base64url over
// session.RequestParts.
const SignatureHeader = "X-Device-Signature"

// caller is a logged-in device, read from the directory.
type caller struct {
	session.Claims
	device directory.Device
}

type callerHandler func(ctx context.Context, req httpapi.Request, c caller) httpapi.Response

// withDevice requires a session token and reads the device and its account,
// so a replaced account or a removed device is refused at once rather than
// when its token expires.
func (s *Server) withDevice(route string, h callerHandler) httpapi.HandlerFunc {
	return func(ctx context.Context, req httpapi.Request) httpapi.Response {
		c, resp, ok := s.authenticate(ctx, route, req)
		if !ok {
			return resp
		}
		if c.device.Replaced {
			return httpapi.Error(http.StatusGone, "account_replaced")
		}
		return h(ctx, req, c)
	}
}

// withSignature is withDevice plus a fresh device signature over the request,
// for sensitive actions: a stolen session token alone can't make them.
func (s *Server) withSignature(route string, h callerHandler) httpapi.HandlerFunc {
	return func(ctx context.Context, req httpapi.Request) httpapi.Response {
		c, resp, ok := s.authenticate(ctx, route, req)
		if !ok {
			return resp
		}
		signed, ok := parseSignature(httpapi.Header(req, SignatureHeader))
		if !ok {
			return httpapi.Error(http.StatusUnauthorized, "signature_rejected")
		}
		body, err := httpapi.Body(req)
		if err != nil {
			return httpapi.Error(http.StatusBadRequest, "bad_request")
		}
		signed.AccountID = c.AccountID
		signed.DeviceID = c.DeviceID
		signed.Method = strings.ToUpper(req.RequestContext.HTTP.Method)
		signed.Path = req.RawPath
		signed.Body = body

		key, err := devicesig.ParsePublicKey(c.device.Key)
		if err != nil {
			return s.internal(route, err)
		}
		err = session.CheckRequest(ctx, signed, key, s.Nonces, s.Now())
		if errors.Is(err, session.ErrLogin) {
			return httpapi.Error(http.StatusUnauthorized, "signature_rejected")
		}
		if err != nil {
			return s.internal(route, err)
		}
		if c.device.Replaced {
			return httpapi.Error(http.StatusGone, "account_replaced")
		}
		return h(ctx, req, c)
	}
}

func (s *Server) authenticate(ctx context.Context, route string, req httpapi.Request) (caller, httpapi.Response, bool) {
	token, ok := strings.CutPrefix(httpapi.Header(req, "Authorization"), "Bearer ")
	if !ok {
		return caller{}, httpapi.Error(http.StatusUnauthorized, "session_expired"), false
	}
	claims, err := s.Sessions.Verify(token, s.Now())
	if err != nil {
		return caller{}, httpapi.Error(http.StatusUnauthorized, "session_expired"), false
	}
	device, err := s.Directory.Device(ctx, claims.AccountID, claims.DeviceID, s.Now())
	if errors.Is(err, directory.ErrNotFound) {
		return caller{}, httpapi.Error(http.StatusUnauthorized, "login_refused"), false
	}
	if err != nil {
		return caller{}, s.internal(route, err), false
	}
	return caller{Claims: claims, device: device}, httpapi.Response{}, true
}

// parseSignature reads "<timestamp>.<nonce>.<signature>".
func parseSignature(header string) (session.SignedRequest, bool) {
	parts := strings.Split(header, ".")
	if len(parts) != 3 {
		return session.SignedRequest{}, false
	}
	ts, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return session.SignedRequest{}, false
	}
	sig, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil {
		return session.SignedRequest{}, false
	}
	return session.SignedRequest{Timestamp: ts, Nonce: parts[1], Signature: sig}, true
}
