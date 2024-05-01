package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"
	"strings"
)

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
	if len(args) == 1 && args[0] == "/start" {
		// Reset user state
		err := r.userRepo.resetUserState(r.user.UUID)
		if err != nil {
			return err
		}

		_, err = b.SendMessage(ctx.EffectiveChat.Id, i18n.T(i18n.StartMessageText), &gotgbot.SendMessageOpts{})
		if err != nil {
			return fmt.Errorf("failed to send bot info: %w", err)
		}
		return nil
	}
	if len(args) == 2 && args[0] == "/start" {

		var err error
		var receiverUser *User
		var identity string

		if strings.HasPrefix(args[1], "_") {
			username := args[1][1:]
			receiverUser, err = r.userRepo.readUserByUsername(username)
		} else {
			receiverUser, err = r.userRepo.readUserByUUID(args[1])
		}

		if err != nil || receiverUser == nil {
			_, err = b.SendMessage(ctx.EffectiveChat.Id, "User not found! Wrong link?", &gotgbot.SendMessageOpts{})
			if err != nil {
				return fmt.Errorf("failed to send bot info: %w", err)
			}
			return nil
		}

		if receiverUser.UUID == r.user.UUID {
			_, err = b.SendMessage(ctx.EffectiveChat.Id, "Do you really want to talk to yourself? So sad! Share your link with friends or post it on social media to get anonymous messages!", &gotgbot.SendMessageOpts{})
			if err != nil {
				return fmt.Errorf("failed to send bot info: %w", err)
			}
			return nil
		}

		// Check if they block each other
		blockedBy := blockCheck(&r.user, receiverUser)
		if blockedBy != None {
			var reason string
			var keyboard gotgbot.InlineKeyboardMarkup
			if blockedBy == Sender {
				reason = "You have blocked this user."
				keyboard = gotgbot.InlineKeyboardMarkup{
					InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
						{
							{
								Text:         "Unblock",
								CallbackData: fmt.Sprintf("ub|%s|%d", receiverUser.UUID, 0),
							},
						},
					},
				}

			} else if blockedBy == Receiver {
				reason = "This user has blocked you."
			}

			_, err = ctx.EffectiveMessage.Reply(b, reason, &gotgbot.SendMessageOpts{
				ReplyMarkup: keyboard,
			})

			if err != nil {
				return fmt.Errorf("failed to send block message: %w", err)
			}

			return nil
		}

		// Set user state to sending
		err = r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":       Sending,
			"ContactUUID": receiverUser.UUID,
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		if receiverUser.Name != "" {
			identity = receiverUser.Name
		} else if receiverUser.Username != "" {
			identity = receiverUser.Username
		} else {
			identity = receiverUser.UUID
		}

		_, err = b.SendMessage(ctx.EffectiveChat.Id, fmt.Sprintf(i18n.T(i18n.InitialSendMessagePromptText), identity), &gotgbot.SendMessageOpts{})
		if err != nil {
			return fmt.Errorf("failed to send bot info: %w", err)
		}
	}

	return nil
}

func (r *RootHandler) info(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := b.SendMessage(ctx.EffectiveChat.Id, "Bugfloyd Anonymous bot\n\nSource code:\nhttps://github.com/bugfloyd/anonymous-telegram-bot", &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	err = r.userRepo.resetUserState(r.user.UUID)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) getLink(b *gotgbot.Bot, ctx *ext.Context) error {
	var link string
	if r.user.Username != "" {
		usernameLink := fmt.Sprintf("https://t.me/%s?start=_%s", b.User.Username, r.user.Username)
		uuidLink := fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, r.user.UUID)
		link = fmt.Sprintf("%s\n\nor:\n\n%s", usernameLink, uuidLink)
	} else {
		link = fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, r.user.UUID)
	}
	_, err := ctx.EffectiveMessage.Reply(b, link, nil)
	if err != nil {
		return err
	}
	err = r.userRepo.resetUserState(r.user.UUID)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) processText(b *gotgbot.Bot, ctx *ext.Context) error {
	switch r.user.State {
	case Sending:
		return r.sendAnonymousMessage(b, ctx)
	case SettingUsername:
		return r.setUsername(b, ctx)
	default:
		return r.sendError(b, ctx, "Unknown Command!")
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
