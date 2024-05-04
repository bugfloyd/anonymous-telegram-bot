package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
	"github.com/aws/aws-lambda-go/events"
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

	// Get sqids alphabet from the environment variable.
	alphabet := os.Getenv("SQIDS_ALPHABET")
	if alphabet == "" {
		return APIResponse{StatusCode: 500}, errors.New("SQIDS_ALPHABET environment variable is empty")
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

	// Commands
	dispatcher.AddHandler(handlers.NewCommand(string(StartCommand), rootHandler.init(StartCommand)))
	dispatcher.AddHandler(handlers.NewCommand(string(InfoCommand), rootHandler.init(InfoCommand)))
	dispatcher.AddHandler(handlers.NewCommand(string(LinkCommand), rootHandler.init(LinkCommand)))
	dispatcher.AddHandler(handlers.NewCommand(string(UsernameCommand), rootHandler.init(UsernameCommand)))
	dispatcher.AddHandler(handlers.NewCommand(string(LanguageCommand), rootHandler.init(LanguageCommand)))
	dispatcher.AddHandler(handlers.NewCommand(string(UnBlockAllCommand), rootHandler.init(UnBlockAllCommand)))

	// Add handler to process all text messages
	dispatcher.AddHandler(handlers.NewMessage(CustomSendMessageFilter, rootHandler.init(TextMessage)))

	// Callback queries handlers
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("r|"), rootHandler.init(ReplyCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("b|"), rootHandler.init(BlockCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("ub|"), rootHandler.init(UnBlockCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("o|"), rootHandler.init(OpenCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("u"), rootHandler.init(SetUsernameCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("ru"), rootHandler.init(RemoveUsernameCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("cu"), rootHandler.init(CancelUsernameCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("l|"), rootHandler.init(SetLanguageCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("lc"), rootHandler.init(CancelLanguageCallback)))

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

func CustomSendMessageFilter(msg *gotgbot.Message) bool {
	// accept all media and messages
	return message.Text(msg) ||
		message.Animation(msg) ||
		message.Audio(msg) ||
		message.Document(msg) ||
		message.Photo(msg) ||
		message.Sticker(msg) ||
		message.Story(msg) ||
		message.Video(msg) ||
		message.VideoNote(msg) ||
		message.Voice(msg) ||
		message.Contact(msg) ||
		message.Location(msg)
}
