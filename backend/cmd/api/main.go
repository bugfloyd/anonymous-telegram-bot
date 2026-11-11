// Command api serves the client's HTTPS API through a Lambda function URL.
package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/incognitochat/incognitochat/backend/internal/api"
	"github.com/incognitochat/incognitochat/backend/internal/config"
	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/ephemeral"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/session"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/initdata"
)

// minSecretLen is the shortest acceptable key from Parameter Store.
const minSecretLen = 32

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
	lambda.Start(router.Serve)
}

func setup(ctx context.Context) (*api.Server, error) {
	env, err := config.Env(os.Getenv,
		"TABLE_DIRECTORY", "TABLE_EPHEMERAL",
		"PARAM_BOT_ID", "PARAM_SESSION_KEY", "PARAM_TELEGRAM_ID_PEPPER")
	if err != nil {
		return nil, err
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	params, err := config.Load(ctx, ssm.NewFromConfig(aws),
		env["PARAM_BOT_ID"], env["PARAM_SESSION_KEY"], env["PARAM_TELEGRAM_ID_PEPPER"])
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
	}, nil
}
