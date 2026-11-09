package common

import (
	"errors"
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/telegram-incognito-bot/common/botinfo"
	"github.com/bugfloyd/telegram-incognito-bot/common/conversation"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/bugfloyd/telegram-incognito-bot/common/invitations"
	"github.com/bugfloyd/telegram-incognito-bot/common/users"
	"github.com/bugfloyd/telegram-incognito-bot/secrets"
	"github.com/sqids/sqids-go"
	"strings"
)

func (r *RootHandler) processUser(userRepo *users.UserRepository, ctx *ext.Context) (*users.User, error) {
	user, err := userRepo.ReadUserByUserId(ctx.EffectiveUser.Id)
	if errors.Is(err, users.ErrNotFound) {
		// First contact: create the user. Any other error is returned, so a
		// transient failure never creates a duplicate user.
		user, err = userRepo.CreateUser(ctx.EffectiveUser.Id)
	}
	if err != nil {
		return nil, err
	}
	return user, nil
}

func (r *RootHandler) start(b *gotgbot.Bot, ctx *ext.Context) error {
	args := ctx.Args()
	if len(args) == 1 && args[0] == "/start" {
		// Reset user state
		err := r.userRepo.ResetState(r.user)
		if err != nil {
			return err
		}

		_, err = b.SendMessage(ctx.EffectiveChat.Id, withLanguageHint(i18n.T(i18n.StartMessageText)), &gotgbot.SendMessageOpts{})
		if err != nil {
			return fmt.Errorf("failed to send bot info: %w", err)
		}
		return nil
	}
	if len(args) == 2 && args[0] == "/start" {
		if code, ok := invitations.CodeFromPayload(args[1]); ok {
			return r.startWithInvitation(b, ctx, code)
		}
		var err error
		var receiverUser *users.User
		var identity string

		if strings.HasPrefix(args[1], "_") {
			username := args[1][1:]
			receiverUser, err = r.userRepo.ReadUserByUsername(username)
			if receiverUser == nil || err != nil {
				fmt.Println("failed to retrieve the link owner:", err)
				_, err = b.SendMessage(ctx.EffectiveChat.Id, i18n.T(i18n.UserNotFoundText), &gotgbot.SendMessageOpts{})
				if err != nil {
					return fmt.Errorf("failed to send wrong link response: %w", err)
				}
				return nil
			}
			identity = receiverUser.Username
		} else {
			linkKey, createdAt, err := readUserLinkKey(args[1])
			if err != nil {
				return fmt.Errorf("failed to read the link key: %w", err)
			}
			receiverUser, err = r.userRepo.ReadUserByLinkKey(linkKey, createdAt)
			if receiverUser == nil || err != nil {
				fmt.Println("failed to retrieve the link owner:", err)
				_, err = b.SendMessage(ctx.EffectiveChat.Id, i18n.T(i18n.UserNotFoundText), &gotgbot.SendMessageOpts{})
				if err != nil {
					return fmt.Errorf("failed to send wrong link response: %w", err)
				}
				return nil
			}
			identity = args[1]
		}

		if receiverUser.UUID == r.user.UUID {
			_, err = b.SendMessage(ctx.EffectiveChat.Id, i18n.T(i18n.MessageToYourselfTextText), &gotgbot.SendMessageOpts{})
			if err != nil {
				return fmt.Errorf("failed to send bot info: %w", err)
			}
			return nil
		}

		// Set user state to sending: whoever opens a link is anonymous to its
		// owner.
		err = r.userRepo.StartSending(r.user, receiverUser.UUID, 0, conversation.Anonymous)
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		_, err = b.SendMessage(ctx.EffectiveChat.Id, fmt.Sprintf(i18n.T(i18n.InitialSendMessagePromptText), identity), &gotgbot.SendMessageOpts{})
		if err != nil {
			return fmt.Errorf("failed to send bot info: %w", err)
		}
	}

	return nil
}

func (r *RootHandler) info(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := b.SendMessage(ctx.EffectiveChat.Id, withLanguageHint(botinfo.Text(b.User)), &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	err = r.userRepo.ResetState(r.user)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) getLink(b *gotgbot.Bot, ctx *ext.Context) error {
	if ok, err := r.mayOwnLink(b, ctx, true); !ok {
		return err
	}
	s, _ := sqids.New(sqids.Options{
		Alphabet: secrets.SqidsAlphabet,
	})
	genericLinkKey, err := s.Encode([]uint64{uint64(r.user.LinkKey), uint64(r.user.CreatedAt.Unix())})
	if err != nil {
		return err
	}

	var link string
	genericLink := fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, genericLinkKey)
	if r.user.Username != "" {
		usernameLink := fmt.Sprintf("https://t.me/%s?start=_%s", b.User.Username, r.user.Username)
		link = fmt.Sprintf("%s\n%s\n\n%s\n\n%s", i18n.T(i18n.LinkText), usernameLink, i18n.T(i18n.OrText), genericLink)
	} else {
		link = fmt.Sprintf("%s\n%s", i18n.T(i18n.LinkText), genericLink)
	}
	_, err = ctx.EffectiveMessage.Reply(b, withLanguageHint(link), nil)
	if err != nil {
		return err
	}
	err = r.userRepo.ResetState(r.user)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) processText(b *gotgbot.Bot, ctx *ext.Context) error {
	switch r.user.State {
	case users.Sending, users.Continuing:
		if !r.user.Role.Valid() {
			// A conversation started before roles existed: the bot can't tell
			// how the receiver knows the sender, so it ends the conversation
			// instead of guessing.
			if err := r.userRepo.ResetState(r.user); err != nil {
				return err
			}
			return r.sendMessageNotSent(b, ctx)
		}
		if r.user.State == users.Sending ||
			conversation.CanFollowUp(r.user.LastSentAt, messageTime(ctx.EffectiveMessage)) {
			return r.sendAnonymousMessage(b, ctx)
		}
		// Too long since their last message: close the conversation instead
		// of guessing that this message is meant for the same person.
		if err := r.userRepo.ResetState(r.user); err != nil {
			return err
		}
		return r.sendMessageNotSent(b, ctx)
	case users.SettingUsername:
		return r.setUsername(b, ctx)
	case users.EnteringInvitationCode:
		return r.redeemInvitation(b, ctx, ctx.EffectiveMessage.Text)
	case users.CreatingInvitation:
		return r.createInvitation(b, ctx)
	default:
		// Only something that looks like a command is an invalid command.
		if strings.HasPrefix(ctx.EffectiveMessage.Text, "/") {
			return r.sendError(b, ctx, i18n.T(i18n.InvalidCommandText))
		}
		return r.sendMessageNotSent(b, ctx)
	}
}

// sendMessageNotSent tells the user their message went nowhere and how to
// send one, instead of reporting an error.
func (r *RootHandler) sendMessageNotSent(b *gotgbot.Bot, ctx *ext.Context) error {
	hint := fmt.Sprintf(i18n.T(i18n.MessageNotSentText), i18n.T(i18n.ReplyButtonText))
	if _, err := ctx.EffectiveMessage.Reply(b, hint, nil); err != nil {
		return fmt.Errorf("failed to send message-not-sent hint: %w", err)
	}
	return nil
}

func (r *RootHandler) sendError(b *gotgbot.Bot, ctx *ext.Context, message string) error {
	errorMessage := fmt.Sprintf(i18n.T(i18n.ErrorText), message)
	_, err := ctx.EffectiveMessage.Reply(b, errorMessage, nil)
	if err != nil {
		return fmt.Errorf("failed to send error message: %w", err)
	}
	return nil
}

func readUserLinkKey(link string) (int32, int64, error) {
	s, err := sqids.New(sqids.Options{
		Alphabet: secrets.SqidsAlphabet,
	})

	if err != nil {
		return 0, 0, fmt.Errorf("failed to read user link key: %w", err)
	}

	numbers := s.Decode(link)
	if len(numbers) != 2 {
		return 0, 0, fmt.Errorf("failed to read user link key")
	}
	return int32(numbers[0]), int64(numbers[1]), nil
}
