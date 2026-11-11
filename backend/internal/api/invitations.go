package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/invitations"
	"github.com/incognitochat/incognitochat/backend/internal/links"
)

const codeAttempts = 5

type invitationCode struct {
	Code string `json:"code"` // "abcd-efgh"
	Uses int    `json:"uses"`
	Left int    `json:"left"`
}

type invitationsResponse struct {
	InviteOnly bool             `json:"inviteOnly"`
	Member     bool             `json:"member"`
	Admin      bool             `json:"admin"`
	Level      int              `json:"level"`
	Unlimited  bool             `json:"unlimited"`
	Remaining  int              `json:"remaining"`
	MaxUses    int              `json:"maxUses"` // the most uses one new code can have now
	Codes      []invitationCode `json:"codes"`
}

type createInvitationRequest struct {
	Uses int `json:"uses"`
}

type redeemRequest struct {
	Code string `json:"code"`
}

type levelAllowance struct {
	Level       int `json:"level"`
	Invitations int `json:"invitations"`
}

type grantRequest struct {
	Link        string `json:"link"` // a link ID of the member
	Invitations int    `json:"invitations"`
}

// allowance returns how many more invitation uses the caller can give, and
// whether that's unlimited (admins).
func (s *Server) allowance(ctx context.Context, d directory.Device) (remaining int, unlimited bool, err error) {
	if s.isAdmin(d) {
		return invitations.AdminMaxUses, true, nil
	}
	if !d.Member {
		return 0, false, nil
	}
	levels, err := s.Directory.Levels(ctx)
	if err != nil {
		return 0, false, err
	}
	return invitations.Remaining(levels[d.Level], d.Granted, d.Allocated), false, nil
}

// listInvitations shows the caller's invitations: how many they can still
// give, and their codes with how much each is used.
func (s *Server) listInvitations(ctx context.Context, _ httpapi.Request, c caller) httpapi.Response {
	const route = "GET /invitations"
	remaining, unlimited, err := s.allowance(ctx, c.device)
	if err != nil {
		return s.internal(route, err)
	}
	codes, err := s.Directory.Invitations(ctx, c.AccountID)
	if err != nil {
		return s.internal(route, err)
	}
	out := invitationsResponse{
		InviteOnly: s.InviteOnly,
		Member:     c.device.Member,
		Admin:      s.isAdmin(c.device),
		Level:      c.device.Level,
		Unlimited:  unlimited,
		Remaining:  remaining,
		MaxUses:    remaining,
		Codes:      make([]invitationCode, len(codes)),
	}
	if unlimited {
		out.Remaining = 0
	}
	for i, code := range codes {
		out.Codes[i] = invitationCode{Code: code.Code, Uses: code.Uses, Left: code.Left}
	}
	return httpapi.JSON(http.StatusOK, out)
}

// createInvitation makes a code for some uses, spending them from the
// caller's invitations (admins: up to AdminMaxUses per code, no budget).
func (s *Server) createInvitation(ctx context.Context, req httpapi.Request, c caller) httpapi.Response {
	const route = "POST /invitations"
	var in createInvitationRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	remaining, unlimited, err := s.allowance(ctx, c.device)
	if err != nil {
		return s.internal(route, err)
	}
	if !unlimited && !c.device.Member {
		return httpapi.Error(http.StatusForbidden, "invitation_required")
	}
	if in.Uses < 1 || in.Uses > invitations.AdminMaxUses {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	if in.Uses > remaining {
		return httpapi.Error(http.StatusConflict, "no_invitations_left")
	}
	ownerLevel, maxAllocated := 0, -1
	if !unlimited {
		levels, err := s.Directory.Levels(ctx)
		if err != nil {
			return s.internal(route, err)
		}
		ownerLevel, maxAllocated = c.device.Level, levels[c.device.Level]+c.device.Granted
	}

	for range codeAttempts {
		code, err := invitations.NewCode()
		if err != nil {
			return s.internal(route, err)
		}
		err = s.Directory.CreateInvitation(ctx, c.AccountID, code, in.Uses, ownerLevel, maxAllocated)
		switch {
		case err == nil:
			return httpapi.JSON(http.StatusCreated, map[string]string{"code": code})
		case errors.Is(err, directory.ErrLimit):
			return httpapi.Error(http.StatusConflict, "no_invitations_left")
		case errors.Is(err, directory.ErrConflict):
			continue // the code is taken: draw another
		default:
			return s.internal(route, err)
		}
	}
	return httpapi.Error(http.StatusConflict, "conflict")
}

// redeem makes the caller a member with a code.
func (s *Server) redeem(ctx context.Context, req httpapi.Request, c caller) httpapi.Response {
	const route = "POST /invitations/redeem"
	var in redeemRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	// Bounds guessing codes; 31^8 codes make that hopeless anyway.
	ok, err := s.Limits.Allow(ctx, "redeem#"+c.AccountID, invitations.RedeemsPerDay, 24*time.Hour)
	if err != nil {
		return s.internal(route, err)
	}
	if !ok {
		return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
	}
	code, valid := invitations.Normalize(in.Code)
	if !valid {
		return httpapi.Error(http.StatusNotFound, "invalid_invitation")
	}
	level, err := s.Directory.Redeem(ctx, c.AccountID, code)
	switch {
	case errors.Is(err, directory.ErrNotFound):
		return httpapi.Error(http.StatusNotFound, "invalid_invitation")
	case errors.Is(err, directory.ErrAlreadyMember):
		return httpapi.Error(http.StatusConflict, "already_member")
	case err != nil:
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusOK, map[string]int{"level": level})
}

// withAdmin limits a handler to admins.
func (s *Server) withAdmin(h callerHandler) callerHandler {
	return func(ctx context.Context, req httpapi.Request, c caller) httpapi.Response {
		if !s.isAdmin(c.device) {
			return httpapi.Error(http.StatusForbidden, "admin_only")
		}
		return h(ctx, req, c)
	}
}

func (s *Server) adminLevels(ctx context.Context, _ httpapi.Request, _ caller) httpapi.Response {
	levels, err := s.Directory.Levels(ctx)
	if err != nil {
		return s.internal("GET /admin/levels", err)
	}
	out := make([]levelAllowance, 0, len(levels))
	for level, n := range levels {
		out = append(out, levelAllowance{Level: level, Invitations: n})
	}
	slices.SortFunc(out, func(a, b levelAllowance) int { return a.Level - b.Level })
	return httpapi.JSON(http.StatusOK, map[string]any{"levels": out})
}

func (s *Server) adminSetLevel(ctx context.Context, req httpapi.Request, _ caller) httpapi.Response {
	var in levelAllowance
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if in.Level < 1 || in.Level > invitations.MaxLevel || in.Invitations < 0 || in.Invitations > invitations.MaxAllowance {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	if err := s.Directory.SetLevel(ctx, in.Level, in.Invitations); err != nil {
		return s.internal("PUT /admin/levels", err)
	}
	return httpapi.JSON(http.StatusOK, map[string]string{})
}

// adminGrant gives a member extra invitations. Admins name the member by one
// of their links, the only handle anyone has on an account.
func (s *Server) adminGrant(ctx context.Context, req httpapi.Request, _ caller) httpapi.Response {
	var in grantRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if !links.ValidID(in.Link) || in.Invitations < 1 || in.Invitations > invitations.MaxAllowance {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	err := s.Directory.Grant(ctx, in.Link, in.Invitations)
	switch {
	case errors.Is(err, directory.ErrNotFound):
		return httpapi.Error(http.StatusNotFound, "not_found")
	case errors.Is(err, directory.ErrNotMember):
		return httpapi.Error(http.StatusConflict, "not_member")
	case err != nil:
		return s.internal("POST /admin/grant", err)
	}
	return httpapi.JSON(http.StatusOK, map[string]string{})
}
