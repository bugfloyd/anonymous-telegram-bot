package common

import (
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/telegram-incognito-bot/common/buttons"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
)

func (r *RootHandler) blockCallback(b *gotgbot.Bot, ctx *ext.Context, ref buttons.Ref) error {
	cb := ctx.Update.CallbackQuery
	keyboard, err := blockedKeyboard(r.user.UUID, ref)
	if err != nil {
		return err
	}

	if err := r.userRepo.Block(r.user, ref.Other); err != nil {
		return fmt.Errorf("failed to block user: %w", err)
	}

	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: i18n.T(i18n.UserBlockedText),
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	if _, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{ReplyMarkup: keyboard}); err != nil {
		return fmt.Errorf("failed to update message markup: %w", err)
	}
	return nil
}

func (r *RootHandler) unBlockCallback(b *gotgbot.Bot, ctx *ext.Context, ref buttons.Ref) error {
	cb := ctx.Update.CallbackQuery
	keyboard, err := messageKeyboard(r.user.UUID, ref)
	if err != nil {
		return err
	}

	if err := r.userRepo.Unblock(r.user, ref.Other); err != nil {
		return fmt.Errorf("failed to unblock user: %w", err)
	}

	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: i18n.T(i18n.UserUnblockedText),
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	if _, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{ReplyMarkup: keyboard}); err != nil {
		return fmt.Errorf("failed to update message markup: %w", err)
	}
	return nil
}

func (r *RootHandler) unBlockAll(b *gotgbot.Bot, ctx *ext.Context) error {
	err := r.userRepo.UnblockAll(r.user)
	if err != nil {
		return fmt.Errorf("failed to unblock all users: %w", err)
	}

	_, err = ctx.EffectiveMessage.Reply(b, i18n.T(i18n.UnblockAllUsersResultText), nil)
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}

	return nil
}
