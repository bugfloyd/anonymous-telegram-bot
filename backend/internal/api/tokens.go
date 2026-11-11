package api

import (
	"context"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

const (
	// TokenBatch is the most tokens one request signs.
	TokenBatch = 50
	// TokensPerDay limits the tokens an account gets per UTC day, and with
	// them the messages it can send to other people.
	TokensPerDay = 200
)

// Issuer signs and checks anonymous send tokens (internal/tokens).
type Issuer interface {
	Sign(blinded []byte) ([]byte, error)
	Check(token []byte, now time.Time) (nonce []byte, forgetAfter time.Time, err error)
	BlindedLen() int
}

// Spent remembers spent tokens.
type Spent interface {
	Spend(ctx context.Context, nonce []byte, forgetAfter time.Time) (bool, error)
}

// issueTokens signs a batch of blinded tokens for a logged-in device. The
// issuer can't see the tokens, so it can't link their later use to the account.
func (s *Server) issueTokens(ctx context.Context, req httpapi.Request, c caller) httpapi.Response {
	const route = "POST /tokens"
	var in struct {
		Blinded []string `json:"blinded"`
	}
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if len(in.Blinded) == 0 || len(in.Blinded) > TokenBatch {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	blinded := make([][]byte, len(in.Blinded))
	for i, b := range in.Blinded {
		raw, err := base64.RawURLEncoding.Strict().DecodeString(b)
		if err != nil || len(raw) != s.Issuer.BlindedLen() {
			return httpapi.Error(http.StatusBadRequest, "bad_request")
		}
		blinded[i] = raw
	}
	ok, err := s.Limits.AllowN(ctx, "tok#"+c.AccountID, len(blinded), TokensPerDay, 24*time.Hour)
	if err != nil {
		return s.internal(route, err)
	}
	if !ok {
		return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
	}
	signatures := make([]string, len(blinded))
	for i, b := range blinded {
		sig, err := s.Issuer.Sign(b)
		if err != nil {
			return httpapi.Error(http.StatusBadRequest, "bad_request")
		}
		signatures[i] = base64.RawURLEncoding.EncodeToString(sig)
	}
	return httpapi.JSON(http.StatusOK, map[string][]string{"signatures": signatures})
}
