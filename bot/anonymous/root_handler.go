package anonymous

import (
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

type Command string

const (
	StartCommand Command = "start"
	InfoCommand  Command = "info"
	LinkCommand  Command = "link"
	TextMessage  Command = "text"
)

type RootHandler struct {
	user     User
	userRepo UserRepository
}

func NewRootHandler() *RootHandler {
	return &RootHandler{}
}

func (r *RootHandler) init(commandName Command) handlers.Response {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		return r.runCommand(b, ctx, commandName)
	}
}

func (r *RootHandler) runCommand(b *gotgbot.Bot, ctx *ext.Context, command Command) error {
	// create user repo
	userRepo, err := NewUserRepository()
	if err != nil {
		return fmt.Errorf("failed to init db repo: %w", err)
	}
	user, err := r.processUser(userRepo, ctx)

	if err != nil || user == nil {
		return fmt.Errorf("failed to process user: %w", err)
	}
	r.user = *user
	r.userRepo = *userRepo

	// Decide which function to call based on the command
	switch command {
	case StartCommand:
		return r.start(b, ctx)
	case InfoCommand:
		return r.info(b, ctx)
	case LinkCommand:
		return r.getLink(b, ctx)
	case TextMessage:
		return r.processText(b, ctx)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func (r *RootHandler) processUser(userRepo *UserRepository, ctx *ext.Context) (*User, error) {
	user, err := userRepo.GetUserByUserId(ctx.EffectiveUser.Id)
	if err != nil {
		user, err = userRepo.SetUser(ctx.EffectiveUser.Id)
		if err != nil {
			return nil, err
		}
	}

	return user, nil
}

func (r *RootHandler) start(b *gotgbot.Bot, ctx *ext.Context) error {
	args := ctx.Args()
	var message string
	if len(args) == 1 && args[0] == "/start" {
		message = fmt.Sprintf("Your UUID: %s", r.user.UUID)
	}
	if len(args) == 2 && args[0] == "/start" {
		message = fmt.Sprintf("You are sending message to:\n%s\n\nYour UUID:\n%s", args[1], r.user.UUID)

		r.user.SetStateToSeding(&r.userRepo, args[1])
	}

	_, err := b.SendMessage(ctx.EffectiveChat.Id, message, &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	return nil
}

func (r *RootHandler) info(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := b.SendMessage(ctx.EffectiveChat.Id, "Bugfloyd Anonymous bot", &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	return nil
}

func (r *RootHandler) getLink(b *gotgbot.Bot, ctx *ext.Context) error {
	link := fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, r.user.UUID)
	_, err := ctx.EffectiveMessage.Reply(b, link, nil)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) processText(b *gotgbot.Bot, ctx *ext.Context) error {

	// swich case on r.user.State
	switch r.user.State {
	case SENDING:
		return r.sendAnonymousMessage(b, ctx)
	default:
		return r.sendError(b, ctx, "Unknown Command")
	}
}

func (r *RootHandler) sendError(b *gotgbot.Bot, ctx *ext.Context, message string) error {
	errorMessage := fmt.Sprintf("Error: %s", message)
	_, err := ctx.EffectiveMessage.Reply(b, errorMessage, nil)
	if err != nil {
		return fmt.Errorf("failed to send error message: %w", err)
	}
	return nil
}

func (r *RootHandler) sendAnonymousMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	receiver, err := r.userRepo.GetUserByUUID(r.user.ContactUUID)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	_, err = b.SendMessage(receiver.UserID, "You have a new message:", &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	// TODO: use CopyMessage method instead of SendMessage
	_, err = b.SendMessage(receiver.UserID, ctx.EffectiveMessage.Text, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "Reply",
						CallbackData: "reply", // TODO: use proper callback data with message id
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	r.user.SetState(&r.userRepo, REGISTERED)

	_, err = ctx.EffectiveMessage.Reply(b, "Message sent", nil)
	if err != nil {
		return fmt.Errorf("failed to send message to sender: %w", err)
	}

	return nil
}
