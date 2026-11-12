package ephemeral

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/incognitochat/incognitochat/backend/internal/dynamotest"
)

func TestSockets(t *testing.T) {
	client := dynamotest.Client(t)
	clock := time.Unix(1_800_000_000, 0)
	s := &Sockets{DB: client, Table: dynamotest.Table(t, client), Now: func() time.Time { return clock }}
	ctx := context.Background()

	if conns, err := s.Connections(ctx, "acct"); err != nil || len(conns) != 0 {
		t.Fatalf("before: %v %v", conns, err)
	}
	for _, c := range []string{"conn1", "conn2"} {
		if err := s.Open(ctx, "acct", c); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Open(ctx, "other", "conn3"); err != nil {
		t.Fatal(err)
	}
	conns, err := s.Connections(ctx, "acct")
	slices.Sort(conns)
	if err != nil || !slices.Equal(conns, []string{"conn1", "conn2"}) {
		t.Fatalf("connections = %v, %v", conns, err)
	}
	// $disconnect knows only the connection.
	if err := s.Close(ctx, "conn1"); err != nil {
		t.Fatal(err)
	}
	if conns, _ := s.Connections(ctx, "acct"); !slices.Equal(conns, []string{"conn2"}) {
		t.Fatalf("after close = %v", conns)
	}
	// Closing twice, or an unknown connection, is fine.
	if err := s.Close(ctx, "conn1"); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(ctx, "acct", "conn2"); err != nil {
		t.Fatal(err)
	}
	if conns, _ := s.Connections(ctx, "acct"); len(conns) != 0 {
		t.Fatalf("after forget = %v", conns)
	}
	// Expired records don't count, even before TTL deletion.
	clock = clock.Add(SocketTTL + time.Second)
	if conns, _ := s.Connections(ctx, "other"); len(conns) != 0 {
		t.Fatalf("expired = %v", conns)
	}
	if conns, _ := s.Connections(ctx, ""); len(conns) != 0 {
		t.Fatalf("empty account = %v", conns)
	}
}
