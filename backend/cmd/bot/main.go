// Command bot is the Telegram bot (design §15). Telegram's webhook reaches it
// through a Lambda function URL; the deploy invokes it directly with a setup
// event, so the bot token and webhook secret never leave the function.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"

	"github.com/incognitochat/incognitochat/backend/internal/bot"
	"github.com/incognitochat/incognitochat/backend/internal/config"
	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
	"github.com/incognitochat/incognitochat/backend/internal/logx"
)

// event is either a function URL request or the deploy's setup call.
type event struct {
	httpapi.Request
	Setup *struct {
		WebhookURL string `json:"webhookUrl"`
	} `json:"setup"`
}

func main() {
	log := logx.New(os.Stderr, "bot")
	b, setup, err := load(context.Background())
	if err != nil {
		log.Error("startup failed", logx.ErrorKind(err))
		os.Exit(1)
	}
	lambda.Start(func(ctx context.Context, raw json.RawMessage) (any, error) {
		out, err := handle(ctx, raw, b, setup.Run)
		if err != nil {
			log.Error("setup failed", logx.ErrorKind(err))
		}
		return out, err
	})
}

// handle routes an event: the deploy's setup call, or a webhook request.
func handle(ctx context.Context, raw json.RawMessage, b *bot.Bot, setup func(context.Context, string) error) (any, error) {
	var e event
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, errors.New("bot: unreadable event")
	}
	if e.Setup != nil {
		if err := setup(ctx, e.Setup.WebhookURL); err != nil {
			return nil, err
		}
		return map[string]bool{"ok": true}, nil
	}
	return b.Handle(e.Request), nil
}

func load(ctx context.Context) (*bot.Bot, bot.Setup, error) {
	env, err := config.Env(os.Getenv, "PARAM_BOT_TOKEN", "PARAM_WEBHOOK_SECRET", "APP_URL")
	if err != nil {
		return nil, bot.Setup{}, err
	}
	aws, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, bot.Setup{}, err
	}
	params, err := config.Load(ctx, ssm.NewFromConfig(aws), env["PARAM_BOT_TOKEN"], env["PARAM_WEBHOOK_SECRET"])
	if err != nil {
		return nil, bot.Setup{}, err
	}
	secret := params[env["PARAM_WEBHOOK_SECRET"]]
	b := &bot.Bot{Secret: secret, AppURL: env["APP_URL"]}
	setup := bot.Setup{Token: params[env["PARAM_BOT_TOKEN"]], Secret: secret, AppURL: env["APP_URL"]}
	return b, setup, nil
}
