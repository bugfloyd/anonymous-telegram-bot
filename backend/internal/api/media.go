package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/ephemeral"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/media"
)

// DownloadsPerDay limits how often one upload can be fetched: the
// recipient needs one or two; more would make it file hosting.
const DownloadsPerDay = 10

// Media is the media bucket (media.Bucket).
type Media interface {
	UploadURL(ctx context.Context, id string, size int64) (string, error)
	DownloadURL(ctx context.Context, id string) (string, error)
	Delete(ctx context.Context, id string) error
}

// Uploads records uploads until they're deleted (ephemeral.Uploads).
type Uploads interface {
	Put(ctx context.Context, id string, up ephemeral.Upload, expires time.Time) error
	Get(ctx context.Context, id string) (ephemeral.Upload, bool, error)
	Delete(ctx context.Context, id string) error
}

type uploadRequest struct {
	Size       int64    `json:"size"`
	DeleteHash bytes64  `json:"deleteHash"` // SHA-256 of the delete secret, which only the message carries
	Tokens     []string `json:"tokens"`     // base64url send tokens, media.TokensFor(size) of them
}

// upload takes payment for an upload and returns where to put it. Like a
// send, it needs no login: anonymous send tokens pay for it (one per started
// 5 MiB), so an upload can't be tied to an account. The app then PUTs
// exactly size bytes of ciphertext to the URL.
func (s *Server) upload(ctx context.Context, req httpapi.Request) httpapi.Response {
	const route = "POST /media"
	var in uploadRequest
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if s.Media == nil {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	if in.Size < 1 || in.Size > media.MaxSize || len(in.DeleteHash) != sha256.Size {
		return httpapi.Error(http.StatusBadRequest, "bad_request")
	}
	if len(in.Tokens) != media.TokensFor(in.Size) {
		return httpapi.Error(http.StatusUnauthorized, "invalid_token")
	}
	// Check every token before spending any.
	now := s.Now()
	type checked struct {
		nonce       []byte
		forgetAfter time.Time
	}
	tokens := make([]checked, len(in.Tokens))
	for i, t := range in.Tokens {
		raw, err := base64.RawURLEncoding.Strict().DecodeString(t)
		if err != nil {
			return httpapi.Error(http.StatusUnauthorized, "invalid_token")
		}
		nonce, forgetAfter, err := s.Issuer.Check(raw, now)
		if err != nil {
			return httpapi.Error(http.StatusUnauthorized, "invalid_token")
		}
		for _, prev := range tokens[:i] {
			if bytes.Equal(prev.nonce, nonce) {
				return httpapi.Error(http.StatusUnauthorized, "invalid_token")
			}
		}
		tokens[i] = checked{nonce, forgetAfter}
	}
	for _, t := range tokens {
		fresh, err := s.Spent.Spend(ctx, t.nonce, t.forgetAfter)
		if err != nil {
			return s.internal(route, err)
		}
		if !fresh {
			return httpapi.Error(http.StatusUnauthorized, "invalid_token")
		}
	}
	id := ids.New()
	if err := s.Uploads.Put(ctx, id, ephemeral.Upload{DeleteHash: in.DeleteHash, Size: in.Size}, now.Add(media.Retention)); err != nil {
		return s.internal(route, err)
	}
	url, err := s.Media.UploadURL(ctx, id, in.Size)
	if err != nil {
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusCreated, map[string]string{"id": id, "url": url})
}

// download returns where to fetch an upload. The ID is in the encrypted
// message only, so knowing it is the permission; the content is encrypted
// anyway.
func (s *Server) download(ctx context.Context, _ httpapi.Request) httpapi.Response {
	const route = "GET /media/{id}"
	id := httpapi.Param(ctx, "id")
	if s.Media == nil || !ids.Valid(id) {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	if _, ok, err := s.Uploads.Get(ctx, id); err != nil {
		return s.internal(route, err)
	} else if !ok {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	ok, err := s.Limits.Allow(ctx, "dl#"+id, DownloadsPerDay, 24*time.Hour)
	if err != nil {
		return s.internal(route, err)
	}
	if !ok {
		return httpapi.Error(http.StatusTooManyRequests, "rate_limited")
	}
	url, err := s.Media.DownloadURL(ctx, id)
	if err != nil {
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusOK, map[string]string{"url": url})
}

// deleteUpload deletes an upload for whoever holds its delete secret: the
// recipient, once the file is safely on their device.
func (s *Server) deleteUpload(ctx context.Context, req httpapi.Request) httpapi.Response {
	const route = "POST /media/{id}/delete"
	id := httpapi.Param(ctx, "id")
	var in struct {
		Secret bytes64 `json:"secret"`
	}
	if err := httpapi.DecodeJSON(req, &in); err != nil {
		return httpapi.DecodeError(err)
	}
	if s.Media == nil || !ids.Valid(id) || len(in.Secret) == 0 {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	up, ok, err := s.Uploads.Get(ctx, id)
	if err != nil {
		return s.internal(route, err)
	}
	hash := sha256.Sum256(in.Secret)
	if !ok || !bytes.Equal(hash[:], up.DeleteHash) {
		return httpapi.Error(http.StatusNotFound, "not_found")
	}
	if err := s.Media.Delete(ctx, id); err != nil {
		return s.internal(route, err)
	}
	if err := s.Uploads.Delete(ctx, id); err != nil {
		return s.internal(route, err)
	}
	return httpapi.JSON(http.StatusOK, map[string]string{})
}
