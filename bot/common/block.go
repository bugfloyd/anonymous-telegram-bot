package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
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

	err := r.userRepo.updateBlacklist(r.user.UUID, "add", receiverUUID)

	if err != nil {
		return fmt.Errorf("failed to block user: %w", err)
	}

	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "User blocked!",
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	_, _, err = cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "Unblock",
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

	err := r.userRepo.updateBlacklist(r.user.UUID, "delete", receiverUUID)

	if err != nil {
		return fmt.Errorf("failed to unblock user: %w", err)
	}

	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "User unblocked!",
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	var replyMessageKey gotgbot.InlineKeyboardButton
	if replyMessageID == "0" {
		replyMessageKey = gotgbot.InlineKeyboardButton{
			Text:         "Send Message",
			CallbackData: fmt.Sprintf("r|%s|%s", receiverUUID, replyMessageID),
		}
	} else {
		replyMessageKey = gotgbot.InlineKeyboardButton{
			Text:         "Reply",
			CallbackData: fmt.Sprintf("r|%s|%s", receiverUUID, replyMessageID),
		}
	}

	_, _, err = cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					replyMessageKey,
					{
						Text:         "Block",
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
	err := r.userRepo.updateBlacklist(r.user.UUID, "clear", "")
	if err != nil {
		return fmt.Errorf("failed to unblock all users: %w", err)
	}

	_, err = ctx.EffectiveMessage.Reply(b, "All users unblocked!", nil)
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}

	return nil
}

func blockCheck(sender *User, receiver *User) BlockedBy {
	if slices.Contains(sender.Blacklist, receiver.UUID) {
		return Sender
	} else if slices.Contains(receiver.Blacklist, sender.UUID) {
		return Receiver
	}
	return None
}
