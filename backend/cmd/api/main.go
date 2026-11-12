// Command api serves the client's HTTPS API through a Lambda function URL.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/incognitochat/incognitochat/backend/internal/api"
	"github.com/incognitochat/incognitochat/backend/internal/config"
	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/ephemeral"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/inbox"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/media"
	"github.com/incognitochat/incognitochat/backend/internal/notices"
	"github.com/incognitochat/incognitochat/backend/internal/push"
	"github.com/incognitochat/incognitochat/backend/internal/session"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/initdata"
	"github.com/incognitochat/incognitochat/backend/internal/tokens"
)

const (
	// minSecretLen is the shortest acceptable key from Parameter Store.
	minSecretLen = 32
	// slowRequest is how long a request takes before it is logged.
	slowRequest = time.Second
)

func main() {
	log := logx.New(os.Stderr, "api")
	server, err := setup(context.Background())
	if err != nil {
		log.Error("startup failed", logx.ErrorKind(err))
		os.Exit(1)
	}
	server.Log = log
	router := httpapi.NewRouter()
	server.Routes(router)
	// Slow requests are logged, with only their route and duration, to size
	// the function from data.
	router.Observe = func(route string, status int, elapsed time.Duration) {
		if elapsed >= slowRequest {
			log.Warn("slow request",
				slog.String(logx.KeyRoute, route),
				slog.Int(logx.KeyStatus, status),
				slog.Int64(logx.KeyDuration, elapsed.Milliseconds()))
		}
	}
	lambda.Start(router.Serve)
}

func setup(ctx context.Context) (*api.Server, error) {
	env, err := config.Env(os.Getenv,
		"TABLE_DIRECTORY", "TABLE_EPHEMERAL", "TABLE_MESSAGES", "NOTIFY_FUNCTION", "WS_ENDPOINT", "MEDIA_BUCKET",
		"PARAM_BOT_ID", "PARAM_SESSION_KEY", "PARAM_TELEGRAM_ID_PEPPER", "PARAM_ISSUER_KEY")
	if err != nil {
		return nil, err
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	params, err := config.Load(ctx, ssm.NewFromConfig(aws),
		env["PARAM_BOT_ID"], env["PARAM_SESSION_KEY"], env["PARAM_TELEGRAM_ID_PEPPER"], env["PARAM_ISSUER_KEY"])
	if err != nil {
		return nil, err
	}

	botID, err := strconv.ParseInt(params[env["PARAM_BOT_ID"]], 10, 64)
	if err != nil || botID <= 0 {
		return nil, errors.New("api: invalid bot ID")
	}
	sessionKey := []byte(params[env["PARAM_SESSION_KEY"]])
	pepper := []byte(params[env["PARAM_TELEGRAM_ID_PEPPER"]])
	if len(pepper) < minSecretLen {
		return nil, errors.New("api: Telegram ID pepper too short")
	}
	issuer, err := session.NewIssuer(map[byte][]byte{1: sessionKey}, 1)
	if err != nil {
		return nil, err
	}
	tokenIssuer, err := tokens.NewIssuer([]byte(params[env["PARAM_ISSUER_KEY"]]))
	if err != nil {
		return nil, err
	}

	// Admins are recognised by the keyed hash of their Telegram ID, the same
	// one their account stores, so the plain IDs go no further than here.
	adminIDs, err := config.TelegramIDs(os.Getenv("ADMIN_TELEGRAM_IDS"))
	if err != nil {
		return nil, err
	}
	admins := make(map[string]bool, len(adminIDs))
	for _, id := range adminIDs {
		admins[directory.TelegramHash(pepper, id)] = true
	}

	db := dynamodb.NewFromConfig(aws)
	wsEndpoint := env["WS_ENDPOINT"]
	// Without this, presigned uploads would carry a checksum of an empty
	// body, and the app's upload would be refused.
	s3Client := s3.NewFromConfig(aws, func(o *s3.Options) {
		o.RequestChecksumCalculation = awsv2.RequestChecksumCalculationWhenRequired
	})
	now := time.Now
	return &api.Server{
		Telegram:   initdata.NewVerifier(botID),
		Sessions:   issuer,
		Directory:  &directory.Store{DB: db, Table: env["TABLE_DIRECTORY"]},
		Nonces:     &ephemeral.Nonces{DB: db, Table: env["TABLE_EPHEMERAL"], Now: now},
		Limits:     &ephemeral.Limiter{DB: db, Table: env["TABLE_EPHEMERAL"], Now: now},
		Pepper:     pepper,
		Now:        now,
		InviteOnly: config.InviteOnly(os.Getenv("INVITE_ONLY")),
		Admins:     admins,
		Inbox:      &inbox.Store{DB: db, Table: env["TABLE_MESSAGES"]},
		Pause:      api.RandomPause,
		Issuer:     tokenIssuer,
		Spent:      &ephemeral.Spent{DB: db, Table: env["TABLE_EPHEMERAL"], Now: now},
		Notices:    &notices.Client{Lambda: awslambda.NewFromConfig(aws), Function: env["NOTIFY_FUNCTION"]},
		Media:      &media.Bucket{Presign: s3.NewPresignClient(s3Client), S3: s3Client, Name: env["MEDIA_BUCKET"]},
		Uploads:    &ephemeral.Uploads{DB: db, Table: env["TABLE_EPHEMERAL"], Now: now},
		Push: &push.Pusher{
			Sockets: &ephemeral.Sockets{DB: db, Table: env["TABLE_EPHEMERAL"], Now: now},
			API: apigatewaymanagementapi.NewFromConfig(aws, func(o *apigatewaymanagementapi.Options) {
				o.BaseEndpoint = &wsEndpoint
			}),
		},
	}, nil
}
