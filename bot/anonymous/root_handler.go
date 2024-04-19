package anonymous

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

type Command string

const (
	StartCommand  Command = "start"
	InfoCommand   Command = "info"
	LinkCommand   Command = "link"
	TextMessage   Command = "text"
	ReplyCallback Command = "reply-callback"
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
	case ReplyCallback:
		return r.replyCallback(b, ctx)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func (r *RootHandler) processUser(userRepo *UserRepository, ctx *ext.Context) (*User, error) {
	user, err := userRepo.readUserByUserId(ctx.EffectiveUser.Id)
	if err != nil {
		user, err = userRepo.createUser(ctx.EffectiveUser.Id)
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
		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":       SENDING,
			"ContactUUID": args[1],
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}
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
	receiver, err := r.userRepo.readUserByUUID(r.user.ContactUUID)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	var replyParameters *gotgbot.ReplyParameters
	msgText := "You have a new message:"
	if r.user.ReplyMessageID != 0 {
		replyParameters = &gotgbot.ReplyParameters{
			MessageId:                r.user.ReplyMessageID,
			AllowSendingWithoutReply: true,
		}

		msgText = "New Reply to your message:"
	}

	_, err = b.SendMessage(receiver.UserID, msgText, &gotgbot.SendMessageOpts{
		ReplyParameters: replyParameters,
	})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	_, err = b.CopyMessage(receiver.UserID, ctx.EffectiveChat.Id, ctx.EffectiveMessage.MessageId, &gotgbot.CopyMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "Reply",
						CallbackData: fmt.Sprintf("r|%s|%d", r.user.UUID, ctx.EffectiveMessage.MessageId),
					},
				},
			},
		},
		ReplyParameters: replyParameters,
	})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	err = r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
		"State": REGISTERED,
	})
	if err != nil {
		return fmt.Errorf("failed to update user state: %w", err)
	}

	_, err = ctx.EffectiveMessage.Reply(b, "Message sent", nil)
	if err != nil {
		return fmt.Errorf("failed to send message to sender: %w", err)
	}

	return nil
}

func (r *RootHandler) replyCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery

	// split the data
	split := strings.Split(cb.Data, "|")
	if len(split) != 3 {
		return fmt.Errorf("invalid callback data: %s", cb.Data)
	}

	uuid := split[1]
	messageID, err := strconv.ParseInt(split[2], 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse message ID: %w", err)
	}

	// store the message id in the user and set status to replying
	err = r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
		"State":          SENDING,
		"ContactUUID":    uuid,
		"ReplyMessageID": messageID,
	})
	if err != nil {
		return fmt.Errorf("failed to update user state: %w", err)
	}

	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "Replying to message...",
	})

	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	_, err = ctx.EffectiveMessage.Reply(b, "Reply to this message:", nil)

	if err != nil {
		return fmt.Errorf("failed to send reply message: %w", err)
	}

	return nil
}
