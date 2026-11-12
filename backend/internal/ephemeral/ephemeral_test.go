package ephemeral

import (
	"context"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/dynamotest"
)

var start = time.Unix(1_800_000_000, 0)

func TestNonces(t *testing.T) {
	client := dynamotest.Client(t)
	clock := start
	n := &Nonces{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return clock }}
	ctx := context.Background()

	use := func(nonce string) bool {
		t.Helper()
		ok, err := n.Use(ctx, nonce, clock.Add(2*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !use("nonce-a") {
		t.Fatal("first use refused")
	}
	if use("nonce-a") {
		t.Fatal("replay accepted")
	}
	if !use("nonce-b") {
		t.Fatal("another nonce refused")
	}
	clock = start.Add(3 * time.Minute)
	if !use("nonce-a") {
		t.Fatal("expired nonce still blocked")
	}
}

func TestLimiter(t *testing.T) {
	client := dynamotest.Client(t)
	clock := start
	l := &Limiter{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return clock }}
	ctx := context.Background()

	allow := func(key string) bool {
		t.Helper()
		ok, err := l.Allow(ctx, key, 2, 24*time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !allow("k") || !allow("k") {
		t.Fatal("refused within the limit")
	}
	if allow("k") {
		t.Fatal("allowed past the limit")
	}
	if !allow("other") {
		t.Fatal("keys aren't separate")
	}
	clock = start.Add(24 * time.Hour)
	if !allow("k") {
		t.Fatal("next window refused")
	}
}

func TestAllowN(t *testing.T) {
	client := dynamotest.Client(t)
	l := &Limiter{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return start }}
	ctx := context.Background()
	for _, tc := range []struct {
		n    int
		want bool
	}{{3, true}, {2, true}, {1, false}, {0, false}, {6, false}} {
		if ok, err := l.AllowN(ctx, "k", tc.n, 5, time.Hour); err != nil || ok != tc.want {
			t.Fatalf("AllowN(%d) = %v, %v; want %v", tc.n, ok, err, tc.want)
		}
	}
}

func TestSpent(t *testing.T) {
	client := dynamotest.Client(t)
	clock := start
	sp := &Spent{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return clock }}
	ctx := context.Background()
	nonce := []byte("0123456789abcdef0123456789abcdef")
	if ok, err := sp.Spend(ctx, nonce, start.Add(time.Hour)); !ok || err != nil {
		t.Fatalf("first spend: %v, %v", ok, err)
	}
	if ok, _ := sp.Spend(ctx, nonce, start.Add(time.Hour)); ok {
		t.Fatal("double spend accepted")
	}
	clock = start.Add(2 * time.Hour)
	if ok, _ := sp.Spend(ctx, nonce, clock.Add(time.Hour)); !ok {
		t.Fatal("record outlived its expiry")
	}
}
