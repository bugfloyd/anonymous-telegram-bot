package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"
	"regexp"
	"strings"
)

func (r *RootHandler) manageUsername(b *gotgbot.Bot, ctx *ext.Context) error {
	var text string
	var buttons [][]gotgbot.InlineKeyboardButton

	if r.user.Username != "" {
		text = fmt.Sprintf(i18n.T(i18n.YourCurrentUsernameText), r.user.Username)
		buttons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         i18n.T(i18n.ChangeUsernameButtonText),
					CallbackData: "u",
				},
				{
					Text:         i18n.T(i18n.RemoveUsernameButtonText),
					CallbackData: "ru",
				},
				{
					Text:         i18n.T(i18n.CancelButtonText),
					CallbackData: "cu",
				},
			},
		}
	} else {
		text = i18n.T(i18n.YouDontHaveAUsernameText)
		buttons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         i18n.T(i18n.SetUsernameButtonText),
					CallbackData: "u",
				},
				{
					Text:         i18n.T(i18n.CancelButtonText),
					CallbackData: "cu",
				},
			},
		}
	}

	_, err := b.SendMessage(ctx.EffectiveChat.Id, text, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: buttons,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send username info: %w", err)
	}

	return nil
}

func (r *RootHandler) usernameCallback(b *gotgbot.Bot, ctx *ext.Context, action string) error {
	cb := ctx.Update.CallbackQuery

	// Remove username command buttons
	_, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{})
	if err != nil {
		return fmt.Errorf("failed to update username message markup: %w", err)
	}

	if action == "CANCEL" {
		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: i18n.T(i18n.NeverMindButtonText),
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	} else if action == "SET" {
		err := r.userRepo.updateUser(r.user, map[string]interface{}{
			"State":          SettingUsername,
			"ContactUUID":    "",
			"ReplyMessageID": 0,
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		// Send reply instruction
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("%s\n\n%s", i18n.T(i18n.UsernameExplanationText), i18n.T(i18n.EnterANewUsernameText)), nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: i18n.T(i18n.SettingUsernameText),
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	} else if action == "REMOVE" {
		err := r.userRepo.updateUser(r.user, map[string]interface{}{
			"State":          Idle,
			"Username":       "",
			"ContactUUID":    "",
			"ReplyMessageID": 0,
		})
		if err != nil {
			return fmt.Errorf("failed to remove username: %w", err)
		}

		_, _, err = cb.Message.EditText(b, i18n.T(i18n.UsernameHasBeenRemovedText), &gotgbot.EditMessageTextOpts{})
		if err != nil {
			return fmt.Errorf("failed to update username message text: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: i18n.T(i18n.UsernameHasBeenRemovedText),
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	}

	return nil
}

func (r *RootHandler) setUsername(b *gotgbot.Bot, ctx *ext.Context) error {
	username := ctx.EffectiveMessage.Text

	if isValidUsername(username) == false {
		// Send username instruction
		_, err := ctx.EffectiveMessage.Reply(b, i18n.T(i18n.InvalidUsernameText), nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
		return nil
	}

	// Convert to lowercase
	username = strings.ToLower(username)

	existingUser, err := r.userRepo.readUserByUsername(username)
	if err != nil || existingUser == nil {
		err := r.userRepo.updateUser(r.user, map[string]interface{}{
			"Username":       username,
			"State":          Idle,
			"ContactUUID":    "",
			"ReplyMessageID": 0,
		})
		if err != nil {
			return fmt.Errorf("failed to update username: %w", err)
		}

		// Send username instruction
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf(i18n.T(i18n.UsernameHasBeenSetText), username), nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
	} else {
		var text string
		if existingUser.UUID != r.user.UUID {
			text = i18n.T(i18n.UsernameExistsText)
		} else {
			text = i18n.T(i18n.SameUsernameText)

			// Reset sender user
			err = r.userRepo.resetUserState(r.user)
			if err != nil {
				return err
			}
		}
		// Send username instruction
		_, err = ctx.EffectiveMessage.Reply(b, text, nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
	}

	return nil
}

func isValidUsername(username string) bool {
	// Check length
	if len(username) < 3 || len(username) > 20 {
		return false
	}

	// Regular expression to check valid characters
	// ^[a-zA-Z0-9_]+$
	// This checks the string consists only of English letters, digits, and underscores
	re := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !re.MatchString(username) {
		return false
	}

	// Check first character (not a digit or underscore)
	firstChar := username[0]
	if firstChar == '_' || ('0' <= firstChar && firstChar <= '9') {
		return false
	}

	return true
}
