package common

import (
	"fmt"
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

type Command string
type CallbackCommand string

const (
	StartCommand      Command = "start"
	InfoCommand       Command = "info"
	LinkCommand       Command = "link"
	UsernameCommand   Command = "username"
	LanguageCommand   Command = "language"
	UnBlockAllCommand Command = "unblockall"
	TextMessage       Command = "text"
)

const (
	ReplyCallback          CallbackCommand = "reply-callback"
	BlockCallback          CallbackCommand = "block-callback"
	UnBlockCallback        CallbackCommand = "unblock-callback"
	OpenCallback           CallbackCommand = "open-callback"
	SetUsernameCallback    CallbackCommand = "set-username-callback"
	RemoveUsernameCallback CallbackCommand = "remove-username-callback"
	CancelUsernameCallback CallbackCommand = "cancel-username-callback"
	SetLanguageCallback    CallbackCommand = "set-language-callback"
	CancelLanguageCallback CallbackCommand = "cancel-language-callback"
)

type BlockedBy string

const (
	Sender   BlockedBy = "same-user"
	Receiver BlockedBy = "other-user"
	None     BlockedBy = "none"
)

type RootHandler struct {
	user     *User
	userRepo UserRepository
}

func NewRootHandler() *RootHandler {
	return &RootHandler{}
}

func (r *RootHandler) init(commandName interface{}) handlers.Response {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		return r.runCommand(b, ctx, commandName)
	}
}

func (r *RootHandler) runCommand(b *gotgbot.Bot, ctx *ext.Context, command interface{}) error {
	// create user repo
	userRepo, err := NewUserRepository()
	if err != nil {
		return fmt.Errorf("failed to init db repo: %w", err)
	}
	user, err := r.processUser(userRepo, ctx)

	if err != nil || user == nil {
		return fmt.Errorf("failed to process user: %w", err)
	}
	r.user = user
	r.userRepo = *userRepo
	i18n.SetLocale(user.Language, ctx.EffectiveUser.LanguageCode)

	switch c := command.(type) {
	case Command:
		switch c {
		case StartCommand:
			return r.start(b, ctx)
		case InfoCommand:
			return r.info(b, ctx)
		case LinkCommand:
			return r.getLink(b, ctx)
		case UsernameCommand:
			return r.manageUsername(b, ctx)
		case LanguageCommand:
			return r.manageLanguage(b, ctx)
		case TextMessage:
			return r.processText(b, ctx)
		case UnBlockAllCommand:
			return r.unBlockAll(b, ctx)
		default:
			return fmt.Errorf("unknown command: %s", c)
		}
	case CallbackCommand:
		// Reset user state if necessary
		if r.user.State != Idle || r.user.ContactUUID != "" || r.user.ReplyMessageID != 0 {
			err := r.userRepo.ResetUserState(r.user)
			if err != nil {
				return err
			}
		}

		switch c {
		case ReplyCallback:
			return r.replyCallback(b, ctx)
		case BlockCallback:
			return r.blockCallback(b, ctx)
		case UnBlockCallback:
			return r.unBlockCallback(b, ctx)
		case OpenCallback:
			return r.openCallback(b, ctx)
		case SetUsernameCallback:
			return r.usernameCallback(b, ctx, "SET")
		case RemoveUsernameCallback:
			return r.usernameCallback(b, ctx, "REMOVE")
		case CancelUsernameCallback:
			return r.usernameCallback(b, ctx, "CANCEL")
		case SetLanguageCallback:
			return r.languageCallback(b, ctx, "SET")
		case CancelLanguageCallback:
			return r.languageCallback(b, ctx, "CANCEL")
		default:
			return fmt.Errorf("unknown command: %s", c)
		}
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}
