package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-lambda-go/events"

	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/session"
)

type memSockets struct {
	open map[string]string // connection -> account
	fail error
}

func (m *memSockets) Open(_ context.Context, account, conn string) error {
	if m.fail != nil {
		return m.fail
	}
	m.open[conn] = account
	return nil
}

func (m *memSockets) Close(_ context.Context, conn string) error {
	delete(m.open, conn)
	return nil
}

func request(route, conn string, headers map[string]string) events.APIGatewayWebsocketProxyRequest {
	var req events.APIGatewayWebsocketProxyRequest
	req.RequestContext.RouteKey = route
	req.RequestContext.ConnectionID = conn
	req.Headers = headers
	return req
}

func TestHandle(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	issuer, _ := session.NewIssuer(map[byte][]byte{1: bytes.Repeat([]byte("k"), 32)}, 1)
	account, device := ids.New(), ids.New()
	token, _, err := issuer.Issue(account, device, now)
	if err != nil {
		t.Fatal(err)
	}
	logs := &bytes.Buffer{}
	sockets := &memSockets{open: map[string]string{}}
	h := &handler{sessions: issuer, sockets: sockets, now: func() time.Time { return now }, log: logx.New(logs, "ws")}
	ctx := context.Background()

	// A good token: recorded, and the subprotocol echoed (browsers require it).
	resp, _ := h.handle(ctx, request("$connect", "c1", map[string]string{"sec-websocket-protocol": "v1, " + token}))
	if resp.StatusCode != http.StatusOK || resp.Headers["Sec-WebSocket-Protocol"] != "v1" || sockets.open["c1"] != account {
		t.Fatalf("connect: %+v, %v", resp, sockets.open)
	}
	// Heartbeats are answered; disconnect forgets.
	if resp, _ := h.handle(ctx, request("$default", "c1", nil)); resp.StatusCode != http.StatusOK {
		t.Fatalf("heartbeat: %d", resp.StatusCode)
	}
	if resp, _ := h.handle(ctx, request("$disconnect", "c1", nil)); resp.StatusCode != http.StatusOK || len(sockets.open) != 0 {
		t.Fatalf("disconnect: %d %v", resp.StatusCode, sockets.open)
	}

	// Refused: no token, wrong protocol, a bad or expired token.
	for _, protocols := range []string{"", "v1", "v2, " + token, token, "v1, nonsense", "v1, " + token + ", extra"} {
		if resp, _ := h.handle(ctx, request("$connect", "c2", map[string]string{"Sec-WebSocket-Protocol": protocols})); resp.StatusCode != http.StatusForbidden {
			t.Errorf("%q: %d", protocols, resp.StatusCode)
		}
	}
	h.now = func() time.Time { return now.Add(time.Hour) }
	if resp, _ := h.handle(ctx, request("$connect", "c3", map[string]string{"Sec-WebSocket-Protocol": "v1, " + token})); resp.StatusCode != http.StatusForbidden {
		t.Errorf("expired token: %d", resp.StatusCode)
	}
	if len(sockets.open) != 0 {
		t.Fatalf("refused connections recorded: %v", sockets.open)
	}

	// A store failure: refused, logged without the connection or token.
	h.now = func() time.Time { return now }
	sockets.fail = errors.New("boom c4 " + token)
	if resp, _ := h.handle(ctx, request("$connect", "c4", map[string]string{"Sec-WebSocket-Protocol": "v1, " + token})); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("store failure: %d", resp.StatusCode)
	}
	if bytes.Contains(logs.Bytes(), []byte("c4")) || bytes.Contains(logs.Bytes(), []byte(token)) {
		t.Fatalf("logs leak: %s", logs.String())
	}
}
