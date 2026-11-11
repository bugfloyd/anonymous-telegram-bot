// Package api implements the client's HTTPS API.
//
// Error responses carry a stable code the client acts on:
//
//	bad_request, unsupported_media_type  the request is malformed
//	init_data_rejected                    Telegram's launch data is invalid, too old, the wrong kind of launch, or another user's
//	signature_rejected                    a device or account signature is invalid
//	invalid_device_list                   the device list is malformed or doesn't match the request
//	account_exists                        the Telegram user has another account; repeat with replace to end it
//	conflict                              a concurrent change won; try again
//	rate_limited                          too many attempts; try later
//	login_refused                         the device can't log in
//	account_replaced                      the account was replaced by a new registration
//	session_expired                       the session token is missing or expired; log in again
//	invitation_required                   only members and admins can own links while invite-only
//	invalid_link                          the link ID or bundle is malformed or badly signed
//	link_limit                            the account has the most links allowed
//	no_invitations_left                   the member has fewer invitations left than asked for
//	invalid_invitation                    no such invitation code, or it is used up
//	already_member                        the account already joined through an invitation
//	not_member                            invitations can only be granted to members
//	admin_only                            only admins can do this
//	invalid_envelope                      the envelope's layout can't be valid
//	mailbox_limit                         the account has the most mailboxes allowed
//	invalid_token                         the send token is missing, invalid, from another week, or spent
//	not_found                             no such route, or no such link (revoked links included)
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
	"github.com/incognitochat/incognitochat/backend/internal/links"
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
	CreateLink(ctx context.Context, accountID string, l directory.Link, max int) error
	Link(ctx context.Context, id string) (directory.Link, error)
	SetLinkKeys(ctx context.Context, accountID, id string, keys []links.Key) error
	SetNotices(ctx context.Context, accountID string, sealedChat []byte, language string) error
	DeleteLink(ctx context.Context, accountID, id string) error
	Levels(ctx context.Context) (map[int]int, error)
	SetLevel(ctx context.Context, level, allowance int) error
	CreateInvitation(ctx context.Context, accountID, code string, uses, ownerLevel, maxAllocated int) error
	Invitations(ctx context.Context, accountID string) ([]directory.Invitation, error)
	Redeem(ctx context.Context, accountID, code string) (int, error)
	Grant(ctx context.Context, linkID string, invitations int) error
	CreateMailboxes(ctx context.Context, accountID string, ids []string, max int) error
	Mailbox(ctx context.Context, id string) (directory.Mailbox, error)
	DiscardMailbox(ctx context.Context, accountID, id string) error
	Devices(ctx context.Context, accountID string, now time.Time) ([]string, error)
}

// Limiter counts uses of a key in fixed windows.
type Limiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
	AllowN(ctx context.Context, key string, n, limit int, window time.Duration) (bool, error)
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

	// InviteOnly limits owning links to members and admins (design D5).
	InviteOnly bool
	// Admins holds the Telegram hashes (directory.TelegramHash) of admins.
	Admins map[string]bool

	// Inbox stores undelivered envelopes.
	Inbox Inbox
	// Pause, if set, runs when a send goes to a mailbox that doesn't exist,
	// so the answer takes about as long as a delivery (RandomPause).
	Pause func()

	// Issuer signs and checks anonymous send tokens; Spent remembers spent ones.
	Issuer Issuer
	Spent  Spent

	// Notices, if set, sends Telegram notices after deliveries.
	Notices Notices
}

// Routes registers the API's routes.
func (s *Server) Routes(r *httpapi.Router) {
	r.Handle(http.MethodPost, "/register", s.register)
	r.Handle(http.MethodPost, "/session", s.session)
	r.Handle(http.MethodGet, "/account", s.withDevice("GET /account", s.account))
	r.Handle(http.MethodPost, "/links", s.withSignature("POST /links", s.createLink))
	r.Handle(http.MethodPut, "/links/{id}/keys", s.withSignature("PUT /links/{id}/keys", s.setLinkKeys))
	r.Handle(http.MethodDelete, "/links/{id}", s.withSignature("DELETE /links/{id}", s.revokeLink))
	r.Handle(http.MethodGet, "/links/{id}", s.linkBundle)
	r.Handle(http.MethodGet, "/invitations", s.withDevice("GET /invitations", s.listInvitations))
	r.Handle(http.MethodPost, "/invitations", s.withSignature("POST /invitations", s.createInvitation))
	r.Handle(http.MethodPost, "/invitations/redeem", s.withDevice("POST /invitations/redeem", s.redeem))
	r.Handle(http.MethodGet, "/admin/levels", s.withDevice("GET /admin/levels", s.withAdmin(s.adminLevels)))
	r.Handle(http.MethodPut, "/admin/levels", s.withSignature("PUT /admin/levels", s.withAdmin(s.adminSetLevel)))
	r.Handle(http.MethodPost, "/admin/grant", s.withSignature("POST /admin/grant", s.withAdmin(s.adminGrant)))
	r.Handle(http.MethodPost, "/mailboxes", s.withDevice("POST /mailboxes", s.createMailboxes))
	r.Handle(http.MethodPost, "/mailboxes/{id}/discard", s.withDevice("POST /mailboxes/{id}/discard", s.discardMailbox))
	r.Handle(http.MethodPost, "/send", s.send)
	r.Handle(http.MethodPost, "/tokens", s.withDevice("POST /tokens", s.issueTokens))
	r.Handle(http.MethodGet, "/inbox", s.withDevice("GET /inbox", s.listInbox))
	r.Handle(http.MethodPost, "/ack", s.withDevice("POST /ack", s.ack))
	r.Handle(http.MethodPost, "/notices", s.withDevice("POST /notices", s.setNotices))
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

func (b bytes64) MarshalJSON() ([]byte, error) {
	return json.Marshal(base64.RawURLEncoding.EncodeToString(b))
}

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
