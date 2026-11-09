package common

import (
	"fmt"
	"github.com/bugfloyd/telegram-incognito-bot/common/buttons"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/bugfloyd/telegram-incognito-bot/common/users"

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

	// Invitations (see invite.go)
	RegisterCommand Command = "register"
	InviteCommand   Command = "invite"
	LevelsCommand   Command = "levels"
	SetLevelCommand Command = "setlevel"
	GrantCommand    Command = "grant"
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

	// Invitations (see invite.go)
	CreateInvitationCallback CallbackCommand = "create-invitation-callback"
	CancelInvitationCallback CallbackCommand = "cancel-invitation-callback"
)

type RootHandler struct {
	user     *users.User
	userRepo users.UserRepository
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
	userRepo, err := users.NewUserRepository()
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
		// The bot answers every command, which ends an open conversation.
		// (Plain messages are routed through TextMessage and decide in
		// processText; buttons reset the state below.)
		if c != TextMessage && r.user.State == users.Continuing {
			if err := r.userRepo.ResetState(r.user); err != nil {
				return err
			}
		}
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
		case RegisterCommand:
			return r.register(b, ctx)
		case InviteCommand:
			return r.invite(b, ctx)
		case LevelsCommand:
			return r.levels(b, ctx)
		case SetLevelCommand:
			return r.setLevel(b, ctx)
		case GrantCommand:
			return r.grant(b, ctx)
		default:
			return fmt.Errorf("unknown command: %s", c)
		}
	case CallbackCommand:
		// Buttons that refer to another user only act on data the bot sealed
		// for the person pressing them. Checked before anything else.
		var ref buttons.Ref
		if action, ok := userButtons[c]; ok {
			var err error
			if ref, ok, err = r.openButton(b, ctx, action); !ok {
				return err
			}
		}

		// Reset user state if necessary
		if r.user.State != users.Idle || r.user.ContactUUID != "" || r.user.ReplyMessageID != 0 {
			err := r.userRepo.ResetState(r.user)
			if err != nil {
				return err
			}
		}

		switch c {
		case ReplyCallback:
			return r.replyCallback(b, ctx, ref)
		case BlockCallback:
			return r.blockCallback(b, ctx, ref)
		case UnBlockCallback:
			return r.unBlockCallback(b, ctx, ref)
		case OpenCallback:
			return r.openCallback(b, ctx, ref)
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
		case CreateInvitationCallback:
			return r.createInvitationCallback(b, ctx)
		case CancelInvitationCallback:
			return r.cancelInvitationCallback(b, ctx)
		default:
			return fmt.Errorf("unknown command: %s", c)
		}
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}
