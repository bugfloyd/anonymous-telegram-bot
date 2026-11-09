package common

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"time"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/telegram-incognito-bot/common/buttons"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/bugfloyd/telegram-incognito-bot/common/notification"
	"github.com/bugfloyd/telegram-incognito-bot/common/users"
)

func (r *RootHandler) sendAnonymousMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	receiver, err := r.userRepo.ReadUserByUUID(r.user.ContactUUID)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	// A block stops messages in both directions, silently. Nobody is ever
	// told about blocks: block status would let people link someone's
	// anonymous messages to their public link. The sender sees the usual
	// "sent" reaction, and a missing "seen" looks the same as not opened yet.
	if slices.Contains(receiver.Blacklist, r.user.UUID) || slices.Contains(r.user.Blacklist, receiver.UUID) {
		if err := reactSent(b, ctx.EffectiveMessage); err != nil {
			return err
		}
		return r.userRepo.ContinueConversation(r.user, messageTime(ctx.EffectiveMessage))
	}

	// What the receiver's buttons refer to: this message, and the sender in
	// the role they have in this conversation.
	ref := buttons.Ref{Other: r.user.UUID, Role: r.user.Role, MessageID: ctx.EffectiveMessage.MessageId}
	keyboard, err := openKeyboard(receiver.UUID, receiver.Language, ref)
	if err != nil {
		return err
	}

	var replyParameters *gotgbot.ReplyParameters
	msgText := fmt.Sprintf(i18n.TT(i18n.NewMessageFromText, receiver.Language), labelOf(receiver.UUID, ref))
	if r.user.ReplyMessageID != 0 {
		replyParameters = &gotgbot.ReplyParameters{
			MessageId:                r.user.ReplyMessageID,
			AllowSendingWithoutReply: true,
		}

		msgText = fmt.Sprintf(i18n.TT(i18n.NewReplyFromText, receiver.Language), labelOf(receiver.UUID, ref))
	}

	if err := reactSent(b, ctx.EffectiveMessage); err != nil {
		return err
	}

	// Send the new message notification to the receiver
	_, err = b.SendMessage(receiver.UserID, msgText, &gotgbot.SendMessageOpts{
		ReplyMarkup:     keyboard,
		ReplyParameters: replyParameters,
	})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	// The receiver just got a message from this conversation, so their own
	// follow-ups in another one (if any) end here. Delivery already
	// succeeded, so a failure is only logged.
	if _, err := r.userRepo.InterruptFollowUps(receiver.UUID, r.user.UUID, r.user.Role); err != nil {
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

func (r *RootHandler) openCallback(b *gotgbot.Bot, ctx *ext.Context, ref buttons.Ref) error {
	cb := ctx.Update.CallbackQuery
	sender, err := r.userRepo.ReadUserByUUID(ref.Other)
	if errors.Is(err, users.ErrNotFound) {
		// The sender's account no longer exists.
		return r.markMessageUnavailable(b, cb, i18n.T(i18n.MessageUnavailableText))
	}
	if err != nil {
		r.answerOpenFailed(b, cb)
		return fmt.Errorf("failed to get sender: %w", err)
	}
	keyboard, err := messageKeyboard(r.user.UUID, ref)
	if err != nil {
		r.answerOpenFailed(b, cb)
		return err
	}
	var replyMessageID int64
	if ctx.EffectiveMessage.ReplyToMessage != nil {
		replyMessageID = ctx.EffectiveMessage.ReplyToMessage.MessageId
	}

	// Copy the sender's message to the receiver
	_, err = b.CopyMessage(ctx.EffectiveChat.Id, sender.UserID, ref.MessageID, &gotgbot.CopyMessageOpts{
		ReplyMarkup: keyboard,
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

	// Mark the sender's message as seen
	_, err = b.SetMessageReaction(sender.UserID, ref.MessageID, &gotgbot.SetMessageReactionOpts{
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
	opened := fmt.Sprintf(i18n.T(i18n.OpenedNotificationText), labelOf(r.user.UUID, ref))
	if err := notification.Retire(b, cb.Message, opened); err != nil {
		log.Println("failed to retire notification:", err)
	}

	return nil
}

func (r *RootHandler) replyCallback(b *gotgbot.Bot, ctx *ext.Context, ref buttons.Ref) error {
	cb := ctx.Update.CallbackQuery

	// Check if receiver exists
	if _, err := r.userRepo.ReadUserByUUID(ref.Other); err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	// Reply in the same conversation: the user's role in it is the opposite
	// of the other person's.
	err := r.userRepo.StartSending(r.user, ref.Other, ref.MessageID, ref.Role.Opposite())
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
