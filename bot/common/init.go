package common

import (
	"encoding/json"
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/message"
	"github.com/aws/aws-lambda-go/events"
	"github.com/bugfloyd/telegram-incognito-bot/common/webhook"
	"github.com/bugfloyd/telegram-incognito-bot/secrets"
	"log"
	"net/http"
)

type APIResponse events.APIGatewayProxyResponse
type APIRequest events.APIGatewayProxyRequest

// InitBot is our lambda handler invoked by the `lambda.Start` function call
func InitBot(request APIRequest) (APIResponse, error) {
	// Reject requests that don't come from Telegram before doing any work.
	if !webhook.ValidRequest(request.Headers, secrets.WebhookSecret) {
		log.Println("rejected request: missing or invalid webhook secret token")
		return APIResponse{StatusCode: http.StatusUnauthorized, Body: "unauthorized"}, nil
	}

	// Create bot from environment value.
	b, err := gotgbot.NewBot(secrets.BotToken, &gotgbot.BotOpts{
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
	for _, c := range []Command{RegisterCommand, InviteCommand, LevelsCommand, SetLevelCommand, GrantCommand} {
		dispatcher.AddHandler(handlers.NewCommand(string(c), rootHandler.init(c)))
	}

	// Add handler to process all text messages
	dispatcher.AddHandler(handlers.NewMessage(CustomSendMessageFilter, rootHandler.init(TextMessage)))

	// Callback queries handlers
	// Buttons that refer to another user (see userButtons); routed on their
	// action prefix. "ub|" must come before the username callbacks' "u".
	for _, c := range []CallbackCommand{ReplyCallback, BlockCallback, UnBlockCallback, OpenCallback} {
		dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix(string(userButtons[c])+"|"), rootHandler.init(c)))
	}
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("u"), rootHandler.init(SetUsernameCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("ru"), rootHandler.init(RemoveUsernameCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("cu"), rootHandler.init(CancelUsernameCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("l|"), rootHandler.init(SetLanguageCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("lc"), rootHandler.init(CancelLanguageCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Equal(createInvitationCallbackData), rootHandler.init(CreateInvitationCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Equal(cancelInvitationCallbackData), rootHandler.init(CancelInvitationCallback)))

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
