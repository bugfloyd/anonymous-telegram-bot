package session

import (
	"context"
	"sync"
	"time"
)

// MemoryNonces is an in-memory NonceStore for tests and local development.
// Production uses a DynamoDB table with a conditional write and a TTL.
type MemoryNonces struct {
	mu   sync.Mutex
	used map[string]time.Time
	now  func() time.Time
}

// NewMemoryNonces returns an empty store using clock for expiry.
func NewMemoryNonces(clock func() time.Time) *MemoryNonces {
	return &MemoryNonces{used: map[string]time.Time{}, now: clock}
}

// Use implements NonceStore.
func (m *MemoryNonces) Use(_ context.Context, nonce string, expires time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if until, ok := m.used[nonce]; ok && m.now().Before(until) {
		return false, nil
	}
	m.used[nonce] = expires
	return true, nil
}
