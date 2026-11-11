package push

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types"
)

type memSockets struct {
	conns     map[string][]string
	forgotten []string
}

func (m *memSockets) Connections(_ context.Context, account string) ([]string, error) {
	return m.conns[account], nil
}

func (m *memSockets) Forget(_ context.Context, account, c string) error {
	m.forgotten = append(m.forgotten, account+"/"+c)
	return nil
}

type memAPI struct {
	sent []string
	fail map[string]error
}

func (m *memAPI) PostToConnection(_ context.Context, in *apigatewaymanagementapi.PostToConnectionInput, _ ...func(*apigatewaymanagementapi.Options)) (*apigatewaymanagementapi.PostToConnectionOutput, error) {
	if err := m.fail[*in.ConnectionId]; err != nil {
		return nil, err
	}
	if string(in.Data) != `{"t":"sync"}` {
		return nil, errors.New("unexpected data")
	}
	m.sent = append(m.sent, *in.ConnectionId)
	return &apigatewaymanagementapi.PostToConnectionOutput{}, nil
}

func TestPush(t *testing.T) {
	sockets := &memSockets{conns: map[string][]string{"a": {"c1", "c2", "c3"}, "b": {"c4"}}}
	api := &memAPI{fail: map[string]error{"c2": &types.GoneException{}, "c4": errors.New("throttled")}}
	p := &Pusher{Sockets: sockets, API: api}
	ctx := context.Background()

	ok, err := p.Push(ctx, "a")
	if !ok || err != nil {
		t.Fatalf("push: %v %v", ok, err)
	}
	if len(api.sent) != 2 || api.sent[0] != "c1" || api.sent[1] != "c3" {
		t.Fatalf("sent = %v", api.sent)
	}
	if len(sockets.forgotten) != 1 || sockets.forgotten[0] != "a/c2" {
		t.Fatalf("forgotten = %v", sockets.forgotten)
	}
	// Nobody took it.
	if ok, err := p.Push(ctx, "b"); ok || err == nil {
		t.Fatalf("failing push: %v %v", ok, err)
	}
	if ok, err := p.Push(ctx, ""); ok || err != nil {
		t.Fatalf("empty account: %v %v", ok, err)
	}
}
