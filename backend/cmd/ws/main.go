// Command ws handles the WebSocket API's $connect and $disconnect routes. The
// socket only carries pushes from the server; every action goes through the
// HTTPS API. Until authentication is built, every connection is refused.
package main

import (
	"context"
	"net/http"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handle(_ context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	if req.RequestContext.RouteKey == "$disconnect" {
		return events.APIGatewayProxyResponse{StatusCode: http.StatusOK}, nil
	}
	return events.APIGatewayProxyResponse{StatusCode: http.StatusForbidden}, nil
}

func main() {
	lambda.Start(handle)
}
