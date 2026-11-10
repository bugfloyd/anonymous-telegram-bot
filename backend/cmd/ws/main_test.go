package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestHandleRefusesConnections(t *testing.T) {
	for route, want := range map[string]int{
		"$connect":    http.StatusForbidden,
		"$disconnect": http.StatusOK,
		"$default":    http.StatusForbidden,
	} {
		var req events.APIGatewayWebsocketProxyRequest
		req.RequestContext.RouteKey = route
		resp, err := handle(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != want {
			t.Errorf("%s: status = %d, want %d", route, resp.StatusCode, want)
		}
	}
}
