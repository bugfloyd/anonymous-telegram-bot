package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"regexp"
	"strings"
)

func (r *RootHandler) manageUsername(b *gotgbot.Bot, ctx *ext.Context) error {
	var text string
	var buttons [][]gotgbot.InlineKeyboardButton

	if r.user.Username != "" {
		text = fmt.Sprintf("Your current username is: %s", r.user.Username)
		buttons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         "Change",
					CallbackData: "u",
				},
				{
					Text:         "Remove",
					CallbackData: "ru",
				},
				{
					Text:         "Cancel",
					CallbackData: "cu",
				},
			},
		}
	} else {
		text = "You don't have a username!"
		buttons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         "Set one",
					CallbackData: "u",
				},
				{
					Text:         "Cancel",
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
			Text: "Never mind!",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	} else if action == "SET" {
		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":          SettingUsername,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		// Send reply instruction
		_, err = ctx.EffectiveMessage.Reply(b, "Create a username that starts with a letter, includes 3-20 characters, and may contain letters, numbers, or underscores (_). Usernames are automatically converted to lowercase. \n\nEnter new username:", nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Setting username...",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	} else if action == "REMOVE" {
		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":          Idle,
			"Username":       nil,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
		})
		if err != nil {
			return fmt.Errorf("failed to remove username: %w", err)
		}

		_, _, err = cb.Message.EditText(b, "Username has been removed!", &gotgbot.EditMessageTextOpts{})
		if err != nil {
			return fmt.Errorf("failed to update username message text: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Username removed!",
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
		_, err := ctx.EffectiveMessage.Reply(b, "The entered username is not valid. Enter another one:", nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
		return nil
	}

	// Convert to lowercase
	username = strings.ToLower(username)

	existingUser, err := r.userRepo.readUserByUsername(username)
	if err != nil || existingUser == nil {
		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"Username":       username,
			"State":          Idle,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
		})
		if err != nil {
			return fmt.Errorf("failed to update username: %w", err)
		}

		// Send username instruction
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Username has been set: %s", username), nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
	} else {
		var text string
		if existingUser.UUID != r.user.UUID {
			text = "The entered username exists. Enter another one:"
		} else {
			text = "You already own this username silly! If you want to change it, run the username command once more!"

			// Reset sender user
			err = r.userRepo.resetUserState(r.user.UUID)
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
