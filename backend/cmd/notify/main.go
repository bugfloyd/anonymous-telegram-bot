// Command notify sends Telegram "New message" notices (design §12). The api
// function invokes it: to seal a chat ID (only notify holds the key), and,
// without waiting, after each delivery. It can read the bot token and the
// chat key, and the notice fields of accounts; nothing about messages.
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/incognitochat/incognitochat/backend/internal/config"
	"github.com/incognitochat/incognitochat/backend/internal/directory"
	"github.com/incognitochat/incognitochat/backend/internal/ephemeral"
	"github.com/incognitochat/incognitochat/backend/internal/ids"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
	"github.com/incognitochat/incognitochat/backend/internal/notices"
	"github.com/incognitochat/incognitochat/backend/internal/telegram/botapi"
)

func main() {
	log := logx.New(os.Stderr, "notify")
	sealer, notifier, err := load(context.Background())
	if err != nil {
		log.Error("startup failed", logx.ErrorKind(err))
		os.Exit(1)
	}
	lambda.Start(func(ctx context.Context, raw json.RawMessage) (any, error) {
		out, err := handle(ctx, raw, sealer, notifier.Notify)
		if err != nil {
			log.Warn("notify failed", logx.ErrorKind(err))
		}
		return out, err
	})
}

func handle(ctx context.Context, raw json.RawMessage, sealer *notices.Sealer, notify func(context.Context, string) error) (any, error) {
	var e notices.Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, errors.New("notify: unreadable event")
	}
	switch {
	case e.Seal != nil:
		if e.Seal.Chat <= 0 {
			return nil, errors.New("notify: bad chat")
		}
		sealed, err := sealer.Seal(e.Seal.Chat)
		if err != nil {
			return nil, err
		}
		return notices.SealResult{Sealed: base64.RawURLEncoding.EncodeToString(sealed)}, nil
	case e.Notify != nil:
		if e.Notify.Account == "" {
			// Sent for sends to unknown mailboxes, so they take as long.
			return nil, nil
		}
		if !ids.Valid(e.Notify.Account) {
			return nil, errors.New("notify: bad account")
		}
		return nil, notify(ctx, e.Notify.Account)
	}
	return nil, errors.New("notify: unknown event")
}

func load(ctx context.Context) (*notices.Sealer, *notices.Notifier, error) {
	env, err := config.Env(os.Getenv, "PARAM_BOT_TOKEN", "PARAM_CHAT_KEY", "TABLE_DIRECTORY", "TABLE_EPHEMERAL", "APP_URL")
	if err != nil {
		return nil, nil, err
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, nil, err
	}
	params, err := config.Load(ctx, ssm.NewFromConfig(aws), env["PARAM_BOT_TOKEN"], env["PARAM_CHAT_KEY"])
	if err != nil {
		return nil, nil, err
	}
	sealer, err := notices.NewSealer([]byte(params[env["PARAM_CHAT_KEY"]]))
	if err != nil {
		return nil, nil, err
	}
	db := dynamodb.NewFromConfig(aws)
	now := time.Now
	return sealer, &notices.Notifier{
		Directory: &directory.Store{DB: db, Table: env["TABLE_DIRECTORY"]},
		Quiet:     &ephemeral.Quiet{DB: db, Table: env["TABLE_EPHEMERAL"], Now: now},
		Sealer:    sealer,
		Bot:       botapi.Client{Token: params[env["PARAM_BOT_TOKEN"]]},
		AppURL:    env["APP_URL"],
		Now:       now,
	}, nil
}
