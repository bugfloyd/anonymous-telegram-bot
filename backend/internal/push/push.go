// Package push tells an account's open apps that something arrived, through
// their WebSocket connections (design §12). The push carries no content: the
// app then fetches its inbox over HTTPS, so a lost push loses nothing.
package push

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types"
)

// Hint is the whole message an app receives.
var Hint = []byte(`{"t":"sync"}`)

// Connections is the socket record store (ephemeral.Sockets).
type Connections interface {
	Connections(ctx context.Context, account string) ([]string, error)
	Forget(ctx context.Context, account, connection string) error
}

// Poster sends to a connection (the API Gateway Management API client).
type Poster interface {
	PostToConnection(context.Context, *apigatewaymanagementapi.PostToConnectionInput, ...func(*apigatewaymanagementapi.Options)) (*apigatewaymanagementapi.PostToConnectionOutput, error)
}

// Pusher pushes hints to open apps.
type Pusher struct {
	Sockets Connections
	API     Poster
}

// Push sends the hint to every open connection of an account and reports
// whether any took it. Connections that are gone are forgotten. An empty
// account looks up nothing, at about the same cost.
func (p *Pusher) Push(ctx context.Context, account string) (bool, error) {
	conns, err := p.Sockets.Connections(ctx, account)
	if err != nil {
		return false, err
	}
	delivered := false
	var errs []error
	for _, c := range conns {
		_, err := p.API.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{ConnectionId: &c, Data: Hint})
		var gone *types.GoneException
		switch {
		case err == nil:
			delivered = true
		case errors.As(err, &gone):
			if err := p.Sockets.Forget(ctx, account, c); err != nil {
				errs = append(errs, err)
			}
		default:
			errs = append(errs, err)
		}
	}
	return delivered, errors.Join(errs...)
}
