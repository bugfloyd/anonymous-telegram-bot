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
	EchoCommand  Command = "echo"
)

type RootHandler struct {
	user     User
	receiver User
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
	user, err := r.processUser(ctx)
	if err != nil || user == nil {
		return fmt.Errorf("failed to process user: %w", err)
	}
	r.user = *user

	// Decide which function to call based on the command
	switch command {
	case StartCommand:
		return r.start(b, ctx)
	case InfoCommand:
		return r.info(b, ctx)
	case LinkCommand:
		return r.getLink(b, ctx)
	case EchoCommand:
		return r.echo(b, ctx)
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func (r *RootHandler) processUser(ctx *ext.Context) (*User, error) {
	userRepo, err := NewUserRepository()
	if err != nil {
		return nil, fmt.Errorf("failed to init db repo: %w", err)
	}

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

func (r *RootHandler) echo(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := ctx.EffectiveMessage.Reply(b, ctx.EffectiveMessage.Text, nil)
	if err != nil {
		return err
	}
	return nil
}
