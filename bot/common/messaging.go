package common

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"strconv"
	"strings"
)

func (r *RootHandler) sendAnonymousMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	receiver, err := r.userRepo.readUserByUUID(r.user.ContactUUID)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	// Check if they block each other
	blockedBy := blockCheck(&r.user, receiver)
	if blockedBy != None {
		var reason string
		if blockedBy == Sender {
			reason = "You have blocked this user."
		} else if blockedBy == Receiver {
			reason = "This user has blocked you."
		}
		_, err = ctx.EffectiveMessage.Reply(b, reason, nil)
		if err != nil {
			return fmt.Errorf("failed to send block message: %w", err)
		}

		// Reset sender user
		err = r.userRepo.resetUserState(r.user.UUID)
		if err != nil {
			return err
		}

		return nil
	}

	var replyParameters *gotgbot.ReplyParameters
	msgText := "You have a new message."
	if r.user.ReplyMessageID != 0 {
		replyParameters = &gotgbot.ReplyParameters{
			MessageId:                r.user.ReplyMessageID,
			AllowSendingWithoutReply: true,
		}

		msgText = "New reply to your message."
	}

	// React with sent emoji to senderMessageID
	_, err = ctx.EffectiveMessage.SetReaction(b, &gotgbot.SetMessageReactionOpts{
		Reaction: []gotgbot.ReactionType{
			gotgbot.ReactionTypeEmoji{
				Emoji: "🕊",
			},
		},
		IsBig: false,
	})
	if err != nil {
		return fmt.Errorf("failed to react to sender's message: %w", err)
	}

	// Send the new message notification to the receiver
	_, err = b.SendMessage(receiver.UserID, msgText, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "Open Message",
						CallbackData: fmt.Sprintf("o|%s|%d", r.user.UUID, ctx.EffectiveMessage.MessageId),
					},
				},
			},
		},
		ReplyParameters: replyParameters,
	})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
	}

	// Reset sender user
	err = r.userRepo.resetUserState(r.user.UUID)
	if err != nil {
		return err
	}

	return nil
}

func (r *RootHandler) openCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery
	split := strings.Split(cb.Data, "|")
	if len(split) != 3 {
		return fmt.Errorf("invalid callback data: %s", cb.Data)
	}
	uuid := split[1]
	sender, err := r.userRepo.readUserByUUID(uuid)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	// Send callback answer to telegram
	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "Message opened!",
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	senderMessageID, err := strconv.ParseInt(split[2], 10, 64)
	if err != nil {
		return fmt.Errorf("failed to parse message ID: %w", err)
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
						Text:         "Reply",
						CallbackData: fmt.Sprintf("r|%s|%d", sender.UUID, senderMessageID),
					},
					{
						Text:         "Block",
						CallbackData: fmt.Sprintf("b|%s|%d", sender.UUID, senderMessageID),
					},
				},
			},
		},
		ReplyParameters: &gotgbot.ReplyParameters{
			MessageId:                replyMessageID,
			AllowSendingWithoutReply: true,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send message to receiver: %w", err)
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
		fmt.Println("failed to react to sender's message: %w", err)
	}

	// Delete message with "Open" button
	_, err = cb.Message.Delete(b, &gotgbot.DeleteMessageOpts{})
	if err != nil {
		fmt.Println("failed to delete message: %w", err)
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
	receiver, err := r.userRepo.readUserByUUID(receiverUUID)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
	}

	// Check if they block each other
	blockedBy := blockCheck(&r.user, receiver)
	if blockedBy != None {
		var reason string
		if blockedBy == Sender {
			reason = "You have blocked this user."
			_, _, err = cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{
				ReplyMarkup: gotgbot.InlineKeyboardMarkup{
					InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
						{
							{
								Text:         "Unblock",
								CallbackData: fmt.Sprintf("ub|%s|%d", receiverUUID, messageID),
							},
						},
					},
				},
			})

			if err != nil {
				return fmt.Errorf("failed to update message markup: %w", err)
			}

		} else if blockedBy == Receiver {
			reason = "This user has blocked you."
		}

		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text:      reason,
			ShowAlert: true,
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}

		return nil
	}

	// Store the message id in the user and set status to replying
	err = r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
		"State":          Sending,
		"ContactUUID":    receiverUUID,
		"ReplyMessageID": messageID,
	})
	if err != nil {
		return fmt.Errorf("failed to update user state: %w", err)
	}

	// Send callback answer to telegram
	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "Replying to message...",
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	// Send reply instruction
	_, err = ctx.EffectiveMessage.Reply(b, "Reply to this message:", nil)
	if err != nil {
		return fmt.Errorf("failed to send reply message: %w", err)
	}

	return nil
}
