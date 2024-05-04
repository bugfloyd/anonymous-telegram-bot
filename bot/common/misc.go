package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"
	"github.com/sqids/sqids-go"
	"os"
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
		err := r.userRepo.resetUserState(r.user)
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
			if err != nil {
				return fmt.Errorf("failed to retrieve the link owner: %w", err)
			}
			identity = receiverUser.Username
		} else {
			linkKey, createdAt, err := readUserLinkKey(args[1])
			if err != nil {
				return fmt.Errorf("failed to read the link key: %w", err)
			}
			receiverUser, err = r.userRepo.readUserByLinkKey(linkKey, createdAt)
			if err != nil {
				return fmt.Errorf("failed to retrieve the link owner: %w", err)
			}
			identity = args[1]
		}

		if receiverUser == nil {
			_, err = b.SendMessage(ctx.EffectiveChat.Id, i18n.T(i18n.UserNotFoundText), &gotgbot.SendMessageOpts{})
			if err != nil {
				return fmt.Errorf("failed to send wrong link response: %w", err)
			}
			return nil
		}

		if receiverUser.UUID == r.user.UUID {
			_, err = b.SendMessage(ctx.EffectiveChat.Id, i18n.T(i18n.MessageToYourselfTextText), &gotgbot.SendMessageOpts{})
			if err != nil {
				return fmt.Errorf("failed to send bot info: %w", err)
			}
			return nil
		}

		// Check if they block each other
		blockedBy := blockCheck(r.user, receiverUser)
		if blockedBy != None {
			var reason string
			var keyboard gotgbot.InlineKeyboardMarkup
			if blockedBy == Sender {
				reason = i18n.T(i18n.YouHaveBlockedThisUserText)
				keyboard = gotgbot.InlineKeyboardMarkup{
					InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
						{
							{
								Text:         i18n.T(i18n.UnblockButtonText),
								CallbackData: fmt.Sprintf("ub|%s|%d", receiverUser.UUID, 0),
							},
						},
					},
				}

			} else if blockedBy == Receiver {
				reason = i18n.T(i18n.ThisUserHasBlockedYouText)
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
		err = r.userRepo.updateUser(r.user, map[string]interface{}{
			"State":       Sending,
			"ContactUUID": receiverUser.UUID,
		})
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
	_, err := b.SendMessage(ctx.EffectiveChat.Id, "Bugfloyd Anonymous bot\n\nSource code:\nhttps://github.com/bugfloyd/anonymous-telegram-bot", &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	err = r.userRepo.resetUserState(r.user)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) getLink(b *gotgbot.Bot, ctx *ext.Context) error {
	alphabet := os.Getenv("SQIDS_ALPHABET")
	s, _ := sqids.New(sqids.Options{
		Alphabet: alphabet,
	})
	genericLinkKey, err := s.Encode([]uint64{uint64(r.user.LinkKey), uint64(r.user.CreatedAt.Unix())})
	if err != nil {
		return err
	}

	var link string
	if r.user.Username != "" {
		usernameLink := fmt.Sprintf("https://t.me/%s?start=_%s", b.User.Username, r.user.Username)
		genericLink := fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, genericLinkKey)
		link = fmt.Sprintf("%s\n%s\n\n%s\n\n%s", i18n.T(i18n.LinkText), usernameLink, i18n.T(i18n.OrText), genericLink)
	} else {
		link = fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, genericLinkKey)
	}
	_, err = ctx.EffectiveMessage.Reply(b, link, nil)
	if err != nil {
		return err
	}
	err = r.userRepo.resetUserState(r.user)
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
		return r.sendError(b, ctx, i18n.T(i18n.InvalidCommandText))
	}
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
	alphabet := os.Getenv("SQIDS_ALPHABET")
	s, err := sqids.New(sqids.Options{
		Alphabet: alphabet,
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
