package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"
	"strings"
)

func (r *RootHandler) manageLanguage(b *gotgbot.Bot, ctx *ext.Context) error {
	var text string
	if r.user.Language != "" {
		text = fmt.Sprintf("Your language is: %s", r.user.Language)
	} else {
		text = "You don't have a preferred language yet."
	}
	_, err := b.SendMessage(ctx.EffectiveChat.Id, text, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "English",
						CallbackData: fmt.Sprintf("l|%s", i18n.EnUS),
					},
					{
						Text:         "Farsi",
						CallbackData: fmt.Sprintf("l|%s", i18n.FaIR),
					},
					{
						Text:         "Cancel",
						CallbackData: "lc",
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send lkanguage info: %w", err)
	}

	return nil
}

func (r *RootHandler) languageCallback(b *gotgbot.Bot, ctx *ext.Context, action string) error {
	cb := ctx.Update.CallbackQuery

	// Remove language command buttons
	_, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{})
	if err != nil {
		return fmt.Errorf("failed to update language message markup: %w", err)
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
		split := strings.Split(cb.Data, "|")
		if len(split) != 2 {
			return fmt.Errorf("invalid callback data: %s", cb.Data)
		}

		var isLanguageValid = false
		for _, lang := range []i18n.Language{i18n.EnUS, i18n.FaIR} {
			if split[1] == string(lang) {
				isLanguageValid = true
				break
			}
		}

		if isLanguageValid == false {
			return fmt.Errorf("invalid language code in callback data: %s", cb.Data)
		}

		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":          Idle,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
			"Language":       split[1],
		})
		if err != nil {
			return fmt.Errorf("failed to update user language: %w", err)
		}

		// Send update status
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Language updated successfully to %s", split[1]), nil)
		if err != nil {
			return fmt.Errorf("failed to send language update message: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Language updated!",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	}
	return nil
}
