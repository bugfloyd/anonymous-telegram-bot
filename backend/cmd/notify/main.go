// Command notify sends generic "New message" notices through the Telegram bot.
// The api function invokes it without waiting, passing only an opaque account
// ID. It is the only function besides bot that can read the bot token. Until
// notices are built, it does nothing.
package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/lambda"
)

func handle(_ context.Context, _ json.RawMessage) error {
	return nil
}

func main() {
	lambda.Start(handle)
}
