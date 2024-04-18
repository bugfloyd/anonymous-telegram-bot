package anonymous

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
	"github.com/aws/aws-lambda-go/events"
	"log"
	"net/http"
	"os"
)

type Response events.APIGatewayProxyResponse
type Request events.APIGatewayProxyRequest

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(request Request) (Response, error) {
	// Get token from the environment variable.
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return Response{StatusCode: 500}, errors.New("TOKEN environment variable is empty")
	}

	// Create bot from environment value.
	b, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{},
		},
	})
	if err != nil {
		return Response{StatusCode: 500}, fmt.Errorf("failed to create new bot: %w", err)
	}

	// Create updater and dispatcher.
	dispatcher := ext.NewDispatcher(&ext.DispatcherOpts{
		// If an error is returned by a handler, log it and continue going.
		Error: func(b *gotgbot.Bot, ctx *ext.Context, err error) ext.DispatcherAction {
			log.Println("an error occurred while handling update:", err.Error())
			return ext.DispatcherActionNoop
		},
		MaxRoutines: ext.DefaultMaxRoutines,
	})

	// /start command to introduce the bot and create the user
	dispatcher.AddHandler(handlers.NewCommand("start", start))
	// /source command to send the bot info
	dispatcher.AddHandler(handlers.NewCommand("info", info))

	// Add echo handler to reply to all text messages.
	dispatcher.AddHandler(handlers.NewMessage(message.Text, echo))

	var update gotgbot.Update
	if err := json.Unmarshal([]byte(request.Body), &update); err != nil {
		log.Println("failed to parse update:", err.Error())
	}

	err = dispatcher.ProcessUpdate(b, &update, nil)
	if err != nil {
		log.Println("failed to process update:", err.Error())
	}

	// Return a successful response with the message
	return Response{
		StatusCode: 200,
		Body:       "success",
	}, nil
}

func start(b *gotgbot.Bot, ctx *ext.Context) error {
	userRepo, err := NewUserRepository()
	if err != nil {
		return fmt.Errorf("failed to init db repo: %w", err)
	}

	user, err := userRepo.GetUserByUserId(ctx.EffectiveUser.Id)
	if err != nil {
		log.Println("failed to get user:", err.Error())
		user, err = userRepo.SetUser(ctx.EffectiveUser.Id)
		if err != nil {
			return err
		}
	}

	_, err = b.SendMessage(ctx.EffectiveChat.Id, fmt.Sprintf("UUID: %s", user.UUID), &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	return nil
}

func info(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := b.SendMessage(ctx.EffectiveChat.Id, "Bugfloyd Anonymous bot", &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	return nil
}

func echo(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := ctx.EffectiveMessage.Reply(b, ctx.EffectiveMessage.Text, nil)
	if err != nil {
		return err
	}
	return nil
}
