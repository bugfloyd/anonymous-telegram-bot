package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"
	"github.com/bugfloyd/anonymous-telegram-bot/common/users"
	"slices"
	"strings"
)

func (r *RootHandler) blockCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery
	split := strings.Split(cb.Data, "|")
	if len(split) != 3 {
		return fmt.Errorf("invalid callback data: %s", cb.Data)
	}
	receiverUUID := split[1]
	replyMessageID := split[2]

	err := r.userRepo.UpdateBlacklist(r.user, "add", receiverUUID)

	if err != nil {
		return fmt.Errorf("failed to block user: %w", err)
	}

	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: i18n.T(i18n.UserBlockedText),
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	_, _, err = cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         i18n.T(i18n.UnblockButtonText),
						CallbackData: fmt.Sprintf("ub|%s|%s", receiverUUID, replyMessageID),
					},
				},
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to update message markup: %w", err)
	}

	return nil
}

func (r *RootHandler) unBlockCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery
	split := strings.Split(cb.Data, "|")
	if len(split) != 3 {
		return fmt.Errorf("invalid callback data: %s", cb.Data)
	}
	receiverUUID := split[1]
	replyMessageID := split[2]

	err := r.userRepo.UpdateBlacklist(r.user, "delete", receiverUUID)

	if err != nil {
		return fmt.Errorf("failed to unblock user: %w", err)
	}

	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: i18n.T(i18n.UserUnblockedText),
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	var replyMessageKey gotgbot.InlineKeyboardButton
	if replyMessageID == "0" {
		replyMessageKey = gotgbot.InlineKeyboardButton{
			Text:         i18n.T(i18n.SendMessageButtonText),
			CallbackData: fmt.Sprintf("r|%s|%s", receiverUUID, replyMessageID),
		}
	} else {
		replyMessageKey = gotgbot.InlineKeyboardButton{
			Text:         i18n.T(i18n.ReplyButtonText),
			CallbackData: fmt.Sprintf("r|%s|%s", receiverUUID, replyMessageID),
		}
	}

	_, _, err = cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					replyMessageKey,
					{
						Text:         i18n.T(i18n.BlockButtonText),
						CallbackData: fmt.Sprintf("b|%s|%s", receiverUUID, replyMessageID),
					},
				},
			},
		},
	})

	if err != nil {
		return fmt.Errorf("failed to update message markup: %w", err)
	}

	return nil
}

func (r *RootHandler) unBlockAll(b *gotgbot.Bot, ctx *ext.Context) error {
	err := r.userRepo.UpdateBlacklist(r.user, "clear", "")
	if err != nil {
		return fmt.Errorf("failed to unblock all users: %w", err)
	}

	_, err = ctx.EffectiveMessage.Reply(b, i18n.T(i18n.UnblockAllUsersResultText), nil)
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}

	return nil
}

func blockCheck(sender *users.User, receiver *users.User) BlockedBy {
	if slices.Contains(sender.Blacklist, receiver.UUID) {
		return Sender
	} else if slices.Contains(receiver.Blacklist, sender.UUID) {
		return Receiver
	}
	return None
}
