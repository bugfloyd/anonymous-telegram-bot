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

type APIResponse events.APIGatewayProxyResponse
type APIRequest events.APIGatewayProxyRequest

// InitBot is our lambda handler invoked by the `lambda.Start` function call
func InitBot(request APIRequest) (APIResponse, error) {
	// Get token from the environment variable.
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		return APIResponse{StatusCode: 500}, errors.New("TOKEN environment variable is empty")
	}

	// Create bot from environment value.
	b, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{},
		},
	})
	if err != nil {
		return APIResponse{StatusCode: 500}, fmt.Errorf("failed to create new bot: %w", err)
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

	rootHandler := NewRootHandler()

	// /start command to introduce the bot and create the user
	dispatcher.AddHandler(handlers.NewCommand(string(StartCommand), rootHandler.init(StartCommand)))
	// /source command to send the bot info
	dispatcher.AddHandler(handlers.NewCommand(string(InfoCommand), rootHandler.init(InfoCommand)))
	// /get_link command to get user entry link
	dispatcher.AddHandler(handlers.NewCommand(string(LinkCommand), rootHandler.init(LinkCommand)))

	// Add echo handler to reply to all text messages.
	dispatcher.AddHandler(handlers.NewMessage(message.Text, rootHandler.init(EchoCommand)))

	var update gotgbot.Update
	if err := json.Unmarshal([]byte(request.Body), &update); err != nil {
		log.Println("failed to parse update:", err.Error())
	}

	err = dispatcher.ProcessUpdate(b, &update, nil)
	if err != nil {
		log.Println("failed to process update:", err.Error())
	}

	// Return a successful response with the message
	return APIResponse{
		StatusCode: 200,
		Body:       "success",
	}, nil
}
