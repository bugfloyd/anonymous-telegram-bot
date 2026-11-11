// Command ws handles the WebSocket API's routes (design §12). A socket only
// carries content-free pushes from the server ("something arrived"); every
// action goes through the HTTPS API, so a dropped socket loses nothing.
//
// $connect checks the session token, sent as the second subprotocol
// (new WebSocket(url, ["v1", token])) so it never appears in a URL, and
// records the connection; $disconnect forgets it; anything else is the app's
// heartbeat, answered so the socket isn't closed as idle.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/incognitochat/incognitochat/backend/internal/config"
	"github.com/incognitochat/incognitochat/backend/internal/ephemeral"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/session"
)

// Protocol is the subprotocol the app asks for, echoed on success.
const Protocol = "v1"

// Sockets records open connections (ephemeral.Sockets).
type Sockets interface {
	Open(ctx context.Context, account, connection string) error
	Close(ctx context.Context, connection string) error
}

type handler struct {
	sessions *session.Issuer
	sockets  Sockets
	now      func() time.Time
	log      *slog.Logger
}

func main() {
	log := logx.New(os.Stderr, "ws")
	h, err := setup(context.Background())
	if err != nil {
		log.Error("startup failed", logx.ErrorKind(err))
		os.Exit(1)
	}
	h.log = log
	lambda.Start(h.handle)
}

func setup(ctx context.Context) (*handler, error) {
	env, err := config.Env(os.Getenv, "PARAM_SESSION_KEY", "TABLE_EPHEMERAL")
	if err != nil {
		return nil, err
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	params, err := config.Load(ctx, ssm.NewFromConfig(aws), env["PARAM_SESSION_KEY"])
	if err != nil {
		return nil, err
	}
	sessions, err := session.NewIssuer(map[byte][]byte{1: []byte(params[env["PARAM_SESSION_KEY"]])}, 1)
	if err != nil {
		return nil, err
	}
	return &handler{
		sessions: sessions,
		sockets:  &ephemeral.Sockets{DB: dynamodb.NewFromConfig(aws), Table: env["TABLE_EPHEMERAL"], Now: time.Now},
		now:      time.Now,
	}, nil
}

func (h *handler) handle(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	conn := req.RequestContext.ConnectionID
	switch req.RequestContext.RouteKey {
	case "$connect":
		token, ok := tokenFrom(header(req.Headers, "Sec-WebSocket-Protocol"))
		if !ok {
			return status(http.StatusForbidden), nil
		}
		claims, err := h.sessions.Verify(token, h.now())
		if err != nil {
			return status(http.StatusForbidden), nil
		}
		if err := h.sockets.Open(ctx, claims.AccountID, conn); err != nil {
			h.log.Warn("connect failed", logx.ErrorKind(err))
			return status(http.StatusInternalServerError), nil
		}
		resp := status(http.StatusOK)
		resp.Headers = map[string]string{"Sec-WebSocket-Protocol": Protocol}
		return resp, nil
	case "$disconnect":
		if err := h.sockets.Close(ctx, conn); err != nil {
			h.log.Warn("disconnect failed", logx.ErrorKind(err))
		}
		return status(http.StatusOK), nil
	default:
		// The heartbeat: nothing to do.
		return status(http.StatusOK), nil
	}
}

// tokenFrom reads the session token from the requested subprotocols:
// exactly "v1" and the token.
func tokenFrom(protocols string) (string, bool) {
	parts := strings.Split(protocols, ",")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) != Protocol {
		return "", false
	}
	token := strings.TrimSpace(parts[1])
	return token, token != ""
}

// header finds a header whatever its case.
func header(headers map[string]string, name string) string {
	for k, v := range headers {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}

func status(code int) events.APIGatewayProxyResponse {
	return events.APIGatewayProxyResponse{StatusCode: code}
}
