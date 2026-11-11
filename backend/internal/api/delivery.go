package api

import (
	"context"
	"encoding/base64"
	"errors"
	"math/rand/v2"
	"net/http"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/envelope"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/inbox"
)

const (
	// MaxMailboxes is how many pool and conversation mailboxes one account can own.
	MaxMailboxes = 200
	// MailboxBatch is the most mailboxes one request creates.
	MailboxBatch = 10
	// MailboxesPerDay limits how many mailboxes an account creates per UTC day.
	MailboxesPerDay = 50
	// SendsPerMailboxPerMinute caps messages to one mailbox.
	SendsPerMailboxPerMinute = 30
	// IntroSendsPerHour caps first messages through one link.
	IntroSendsPerHour = 60
	// InboxPage is the most items one GET /inbox returns.
	InboxPage = 50
	// AckBatch is the most items one POST /ack confirms.
	AckBatch = 100
)

// Inbox stores undelivered envelopes.
type Inbox interface {
	Deliver(ctx context.Context, accountID string, devices []string, mailbox string, envelope []byte, now time.Time) error
	List(ctx context.Context, accountID, deviceID string, limit int, now time.Time) ([]inbox.Item, bool, error)
	Ack(ctx context.Context, accountID, deviceID string, itemIDs []string) error
}

type sendRequest struct {
	Mailbox  string `json:"mailbox"`
	Envelope string `json:"envelope"` // base64url
	Token    string `json:"token"`    // base64url, an anonymous send token
	// Quiet sends trigger no Telegram notice (seen receipts).
	Quiet bool `json:"quiet"`
}

type inboxItem struct {
	ID       string  `json:"id"`
	Mailbox  string  `json:"mailbox"`
	Envelope bytes64 `json:"envelope"`
}

// createMailboxes adds mailboxes to the caller's pool. Clients keep a few
// spare and take one when they need it, so creating one never happens right
// before an anonymous send (design §9).
func (s *Server) createMailboxes(ctx context.Context, req httpapi.Request, c caller) httpapi.Response {
	const route = "POST /mailboxes"
	var in struct {
		Count int `json:"count"`
	}
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if in.Count < 1 || in.Count > MailboxBatch {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	ok, err := s.Limits.Allow(ctx, "mbx#"+c.AccountID, MailboxesPerDay, 24*time.Hour)
	if err != nil {
		return s.internal(route, err)
	}
	if !ok {
		return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
	}
	created := make([]string, in.Count)
	for i := range created {
		created[i] = ids.New()
	}
	err = s.Directory.CreateMailboxes(ctx, c.AccountID, created, MaxMailboxes)
	switch {
	case errors.Is(err, directory.ErrLimit):
		return httpapi.Error(http.StatusConflict, "mailbox_limit")
	case errors.Is(err, directory.ErrConflict):
		return httpapi.Error(http.StatusConflict, "conflict")
	case err != nil:
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusCreated, map[string][]string{"mailboxes": created})
}

// discardMailbox ends a mailbox (a block, or a conversation ended): sends to
// it then go nowhere, answered exactly like sends to a mailbox that never existed.
func (s *Server) discardMailbox(ctx context.Context, _ httpapi.Request, c caller) httpapi.Response {
	id := httpapi.Param(ctx, "id")
	if !ids.Valid(id) {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	err := s.Directory.DiscardMailbox(ctx, c.AccountID, id)
	if errors.Is(err, directory.ErrNotFound) {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	if err != nil {
		return s.internal("POST /mailboxes/{id}/discard", err)
	}
	return httpapi.JSON(http.StatusOK, map[string]string{})
}

// send delivers an envelope to a mailbox's owner, without logging in: the
// server learns the recipient (to deliver) but not the sender. Each send
// spends one anonymous token (tokens.go), which can't be linked to the
// account it was issued to. Unknown,
// discarded and blocked mailboxes get the same answer, in similar time, and
// nothing is stored for them.
func (s *Server) send(ctx context.Context, req httpapi.Request) httpapi.Response {
	const route = "POST /send"
	var in sendRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if !ids.Valid(in.Mailbox) {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	env, err := base64.RawURLEncoding.Strict().DecodeString(in.Envelope)
	if err != nil {
		return httpapi.Error(http.StatusBadRequest, "invalid_envelope")
	}
	if _, err := envelope.Check(env); err != nil {
		return httpapi.Error(http.StatusBadRequest, "invalid_envelope")
	}
	now := s.Now()
	token, err := base64.RawURLEncoding.Strict().DecodeString(in.Token)
	if err != nil {
		return httpapi.Error(http.StatusUnauthorized, "invalid_token")
	}
	nonce, forgetAfter, err := s.Issuer.Check(token, now)
	if err != nil {
		return httpapi.Error(http.StatusUnauthorized, "invalid_token")
	}
	ok, err := s.Limits.Allow(ctx, "send#"+in.Mailbox, SendsPerMailboxPerMinute, time.Minute)
	if err != nil {
		return s.internal(route, err)
	}
	if !ok {
		// The token isn't spent: the sender can try again later.
		return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
	}
	fresh, err := s.Spent.Spend(ctx, nonce, forgetAfter)
	if err != nil {
		return s.internal(route, err)
	}
	if !fresh {
		return httpapi.Error(http.StatusUnauthorized, "invalid_token")
	}

	mailbox, err := s.Directory.Mailbox(ctx, in.Mailbox)
	if errors.Is(err, directory.ErrNotFound) {
		if s.Pause != nil {
			s.Pause()
		}
		// The same requests a delivery makes, so the answer takes as long.
		s.push(ctx, "")
		if !in.Quiet {
			s.notify(ctx, "")
		}
		return accepted()
	}
	if err != nil {
		return s.internal(route, err)
	}
	if mailbox.Link != "" {
		ok, err := s.Limits.Allow(ctx, "intro#"+mailbox.Link, IntroSendsPerHour, time.Hour)
		if err != nil {
			return s.internal(route, err)
		}
		if !ok {
			return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
		}
	}
	devices, err := s.Directory.Devices(ctx, mailbox.Owner, now)
	if err != nil {
		return s.internal(route, err)
	}
	if err := s.Inbox.Deliver(ctx, mailbox.Owner, devices, in.Mailbox, env, now); err != nil {
		return s.internal(route, err)
	}
	// Open apps fetch at once; a notice only if none is open. Receipts are
	// pushed too (they update check marks), but never noticed.
	pushed := s.push(ctx, mailbox.Owner)
	if !in.Quiet {
		if pushed {
			s.notify(ctx, "")
		} else {
			s.notify(ctx, mailbox.Owner)
		}
	}
	return accepted()
}

func accepted() httpapi.Response {
	return httpapi.JSON(http.StatusOK, map[string]string{})
}

// RandomPause stands in for the delivery work skipped for unknown mailboxes,
// so their answer takes about as long.
func RandomPause() {
	time.Sleep(time.Duration(15+rand.IntN(25)) * time.Millisecond)
}

// listInbox returns the calling device's undelivered envelopes.
func (s *Server) listInbox(ctx context.Context, _ httpapi.Request, c caller) httpapi.Response {
	items, more, err := s.Inbox.List(ctx, c.AccountID, c.DeviceID, InboxPage, s.Now())
	if err != nil {
		return s.internal("GET /inbox", err)
	}
	out := make([]inboxItem, len(items))
	for i, it := range items {
		out[i] = inboxItem{ID: it.ID, Mailbox: it.Mailbox, Envelope: it.Envelope}
	}
	return httpapi.JSON(http.StatusOK, map[string]any{"items": out, "more": more})
}

// ack deletes envelopes the calling device has stored.
func (s *Server) ack(ctx context.Context, req httpapi.Request, c caller) httpapi.Response {
	var in struct {
		IDs []string `json:"ids"`
	}
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if len(in.IDs) == 0 || len(in.IDs) > AckBatch {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	for _, id := range in.IDs {
		if !ids.Valid(id) {
			return httpapi.Error(http.StatusBadRequest, "bad_request")
		}
	}
	if err := s.Inbox.Ack(ctx, c.AccountID, c.DeviceID, in.IDs); err != nil {
		return s.internal("POST /ack", err)
	}
	return httpapi.JSON(http.StatusOK, map[string]string{})
}
