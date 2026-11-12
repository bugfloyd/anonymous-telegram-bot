package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/ephemeral"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/media"
)

type memMedia struct {
	deleted []string
}

func (m *memMedia) UploadURL(_ context.Context, id string, size int64) (string, error) {
	return "https://bucket.example/m/" + id + "?put", nil
}

func (m *memMedia) DownloadURL(_ context.Context, id string) (string, error) {
	return "https://bucket.example/m/" + id + "?get", nil
}

func (m *memMedia) Delete(_ context.Context, id string) error {
	m.deleted = append(m.deleted, id)
	return nil
}

type memUploads struct {
	mu  sync.Mutex
	ups map[string]ephemeral.Upload
}

func (m *memUploads) Put(_ context.Context, id string, up ephemeral.Upload, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ups[id] = up
	return nil
}

func (m *memUploads) Get(_ context.Context, id string) (ephemeral.Upload, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	up, ok := m.ups[id]
	return up, ok, nil
}

func (m *memUploads) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.ups, id)
	return nil
}

func TestMedia(t *testing.T) {
	h := newHarness(t)
	store, uploads := &memMedia{}, &memUploads{ups: map[string]ephemeral.Upload{}}
	h.server.Media, h.server.Uploads = store, uploads
	secret := []byte("the delete secret, from the message")
	hash := sha256.Sum256(secret)
	tokens := func(n int) []any {
		out := make([]any, n)
		for i := range out {
			out[i] = mintToken(t, h.now)
		}
		return out
	}

	// 6 MiB costs two tokens; no login needed.
	size := int64(6 << 20)
	status, out := h.post(t, "/media", map[string]any{"size": size, "deleteHash": b64(hash[:]), "tokens": tokens(2)})
	if status != http.StatusCreated {
		t.Fatalf("upload: %d %v", status, out)
	}
	id := out["id"].(string)
	if !ids.Valid(id) || out["url"] != "https://bucket.example/m/"+id+"?put" {
		t.Fatalf("upload = %v", out)
	}
	if up := uploads.ups[id]; up.Size != size || !bytes.Equal(up.DeleteHash, hash[:]) {
		t.Fatalf("record = %+v", up)
	}

	// Anyone with the ID can fetch it (it's encrypted), a few times a day.
	status, out = h.call(t, nil, "", http.MethodGet, "/media/"+id, nil, false)
	if status != http.StatusOK || out["url"] != "https://bucket.example/m/"+id+"?get" {
		t.Fatalf("download: %d %v", status, out)
	}
	for range DownloadsPerDay - 1 {
		h.call(t, nil, "", http.MethodGet, "/media/"+id, nil, false)
	}
	status, out = h.call(t, nil, "", http.MethodGet, "/media/"+id, nil, false)
	wantError(t, status, out, http.StatusTooManyRequests, "rate_limited")

	// Only the delete secret deletes it.
	status, out = h.post(t, "/media/"+id+"/delete", map[string]any{"secret": b64([]byte("wrong"))})
	wantError(t, status, out, http.StatusNotFound, "not_found")
	if status, out := h.post(t, "/media/"+id+"/delete", map[string]any{"secret": b64(secret)}); status != http.StatusOK {
		t.Fatalf("delete: %d %v", status, out)
	}
	if len(store.deleted) != 1 || store.deleted[0] != id {
		t.Fatalf("deleted = %v", store.deleted)
	}
	status, out = h.call(t, nil, "", http.MethodGet, "/media/"+id, nil, false)
	wantError(t, status, out, http.StatusNotFound, "not_found")
}

func TestUploadRejects(t *testing.T) {
	h := newHarness(t)
	h.server.Media, h.server.Uploads = &memMedia{}, &memUploads{ups: map[string]ephemeral.Upload{}}
	hash := sha256.Sum256([]byte("x"))
	upload := func(size int64, deleteHash []byte, tokens []any) (int, map[string]any) {
		return h.post(t, "/media", map[string]any{"size": size, "deleteHash": b64(deleteHash), "tokens": tokens})
	}
	one := mintToken(t, h.now)

	for _, size := range []int64{0, media.MaxSize + 1} {
		status, out := upload(size, hash[:], []any{one})
		wantError(t, status, out, http.StatusBadRequest, "bad_request")
	}
	status, out := upload(10, []byte("short"), []any{one})
	wantError(t, status, out, http.StatusBadRequest, "bad_request")
	// Too few tokens, the same token twice, a bad token.
	status, out = upload(6<<20, hash[:], []any{one})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token")
	status, out = upload(6<<20, hash[:], []any{one, one})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token")
	status, out = upload(10, hash[:], []any{"nope"})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token")
	// A token pays once: for this upload, then not again.
	if status, out := upload(10, hash[:], []any{one}); status != http.StatusCreated {
		t.Fatalf("upload: %d %v", status, out)
	}
	status, out = upload(10, hash[:], []any{one})
	wantError(t, status, out, http.StatusUnauthorized, "invalid_token")
	// Unknown uploads.
	status, out = h.call(t, nil, "", http.MethodGet, "/media/"+ids.New(), nil, false)
	wantError(t, status, out, http.StatusNotFound, "not_found")
	status, out = h.post(t, "/media/"+ids.New()+"/delete", map[string]any{"secret": b64([]byte("x"))})
	wantError(t, status, out, http.StatusNotFound, "not_found")
}
