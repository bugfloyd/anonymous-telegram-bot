package common

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/bugfloyd/telegram-incognito-bot/common/notification"
	"github.com/bugfloyd/telegram-incognito-bot/common/users"
)

func (r *RootHandler) sendAnonymousMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	receiver, err := r.userRepo.ReadUserByUUID(r.user.ContactUUID)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	// Blocking only stops incoming messages, and nobody is ever told they are
	// blocked: block status would let people link someone's anonymous
	// messages to their public link. A message to someone who blocked the
	// sender is silently not delivered; the sender sees the usual "sent"
	// reaction, and missing "seen" looks the same as not opened yet.
	if slices.Contains(receiver.Blacklist, r.user.UUID) {
		if err := reactSent(b, ctx.EffectiveMessage); err != nil {
			return err
		}
		return r.userRepo.ContinueConversation(r.user, messageTime(ctx.EffectiveMessage))
	}

	var replyParameters *gotgbot.ReplyParameters
	msgText := i18n.TT(i18n.YouHaveANewMessageText, receiver.Language)
	if r.user.ReplyMessageID != 0 {
		replyParameters = &gotgbot.ReplyParameters{
			MessageId:                r.user.ReplyMessageID,
			AllowSendingWithoutReply: true,
		}

		msgText = i18n.TT(i18n.NewReplyToYourMessageText, receiver.Language)
	}

	if err := reactSent(b, ctx.EffectiveMessage); err != nil {
		return err
	}

	// Send the new message notification to the receiver
	_, err = b.SendMessage(receiver.UserID, msgText, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         i18n.TT(i18n.OpenMessageButtonText, receiver.Language),
						CallbackData: signButton(receiver.UUID, fmt.Sprintf("o|%s|%d", r.user.UUID, ctx.EffectiveMessage.MessageId)),
					},
				},
			},
		},
		ReplyParameters: replyParameters,
	})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	// The receiver just got a message from this sender, so their own
	// follow-ups to someone else (if any) end here. Delivery already
	// succeeded, so a failure is only logged.
	if _, err := r.userRepo.InterruptFollowUps(receiver.UUID, r.user.UUID); err != nil {
		log.Println("failed to interrupt receiver's follow-ups:", err)
	}

	// Keep the conversation open: the sender's next messages go to the same
	// person until something else happens in their chat.
	return r.userRepo.ContinueConversation(r.user, messageTime(ctx.EffectiveMessage))
}

// markMessageUnavailable turns an Open notification into a visible note that
// the message can't be opened anymore (removing its button), and shows the
// same text as the button's pop-up.
func (r *RootHandler) markMessageUnavailable(b *gotgbot.Bot, cb *gotgbot.CallbackQuery, text string) error {
	if err := notification.MarkUnavailable(b, cb.Message, text); err != nil {
		log.Println(err)
	}
	if _, err := cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: text}); err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}
	return nil
}

// answerOpenFailed tells the user opening didn't work, keeping the
// notification so they can try again.
func (r *RootHandler) answerOpenFailed(b *gotgbot.Bot, cb *gotgbot.CallbackQuery) {
	if _, err := cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: i18n.T(i18n.OpenFailedText)}); err != nil {
		log.Println("failed to answer callback:", err)
	}
}

// reactSent marks the sender's message with the "sent" reaction.
func reactSent(b *gotgbot.Bot, msg *gotgbot.Message) error {
	_, err := msg.SetReaction(b, &gotgbot.SetMessageReactionOpts{
		Reaction: []gotgbot.ReactionType{gotgbot.ReactionTypeEmoji{Emoji: "🕊"}},
	})
	if err != nil {
		return fmt.Errorf("failed to react to sender's message: %w", err)
	}
	return nil
}

// messageTime is when Telegram says the user sent the message.
func messageTime(msg *gotgbot.Message) time.Time {
	return time.Unix(msg.Date, 0)
}

func (r *RootHandler) openCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery
	split := strings.Split(cb.Data, "|")
	if len(split) != 3 {
		return fmt.Errorf("invalid callback data: %s", cb.Data)
	}
	uuid := split[1]
	senderMessageID, err := strconv.ParseInt(split[2], 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse message ID: %w", err)
	}
	sender, err := r.userRepo.ReadUserByUUID(uuid)
	if errors.Is(err, users.ErrNotFound) {
		// The sender's account no longer exists.
		return r.markMessageUnavailable(b, cb, i18n.T(i18n.MessageUnavailableText))
	}
	if err != nil {
		r.answerOpenFailed(b, cb)
		return fmt.Errorf("failed to get sender: %w", err)
	}
	var replyMessageID int64
	if ctx.EffectiveMessage.ReplyToMessage != nil {
		replyMessageID = ctx.EffectiveMessage.ReplyToMessage.MessageId
	}

	// Copy the sender's message to the receiver
	_, err = b.CopyMessage(ctx.EffectiveChat.Id, sender.UserID, senderMessageID, &gotgbot.CopyMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         i18n.T(i18n.ReplyButtonText),
						CallbackData: signButton(r.user.UUID, fmt.Sprintf("r|%s|%d", sender.UUID, senderMessageID)),
					},
					{
						Text:         i18n.T(i18n.BlockButtonText),
						CallbackData: signButton(r.user.UUID, fmt.Sprintf("b|%s|%d", sender.UUID, senderMessageID)),
					},
				},
			},
		},
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId:                replyMessageID,
			AllowSendingWithoutReply: true,
		},
	})
	if notification.SourceGone(err) {
		// The sender deleted the message (for both sides) before it was opened.
		return r.markMessageUnavailable(b, cb, i18n.T(i18n.MessageDeletedText))
	}
	if err != nil {
		r.answerOpenFailed(b, cb)
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	// Only now confirm: the message really was delivered.
	if _, err := cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: i18n.T(i18n.MessageOpenedText)}); err != nil {
		log.Println("failed to answer callback:", err)
	}

	// React with eyes emoji to senderMessageID
	_, err = b.SetMessageReaction(sender.UserID, senderMessageID, &gotgbot.SetMessageReactionOpts{
		Reaction: []gotgbot.ReactionType{
			gotgbot.ReactionTypeEmoji{
				Emoji: "👀",
			},
		},
		IsBig: false,
	})
	if err != nil {
		log.Println("failed to react to sender's message:", err)
	}

	// Remove the notification with the "Open" button: delete it, or, when it's
	// too old to delete, replace its text and drop the button.
	if err := notification.Retire(b, cb.Message, i18n.T(i18n.OpenedNotificationText)); err != nil {
		log.Println("failed to retire notification:", err)
	}

	return nil
}

func (r *RootHandler) replyCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery
	split := strings.Split(cb.Data, "|")
	if len(split) != 3 {
		return fmt.Errorf("invalid callback data: %s", cb.Data)
	}
	receiverUUID := split[1]
	messageID, err := strconv.ParseInt(split[2], 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse message ID: %w", err)
	}

	// Check if receiver exists
	if _, err := r.userRepo.ReadUserByUUID(receiverUUID); err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	// Store the message id in the user and set status to replying
	err = r.userRepo.StartSending(r.user, receiverUUID, messageID)
	if err != nil {
		return fmt.Errorf("failed to update user state: %w", err)
	}

	// Send callback answer to telegram
	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: i18n.T(i18n.ReplyingToMessageText),
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	// Send reply instruction
	_, err = ctx.EffectiveMessage.Reply(b, i18n.T(i18n.ReplyToThisMessageText), nil)
	if err != nil {
		return fmt.Errorf("failed to send reply message: %w", err)
	}

	return nil
}
