// Package notification retires the "you have a new message" notification
// once the recipient has opened the message.
package notification

import (
	"errors"
	"fmt"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// Message is what Retire needs from a Telegram message.
// gotgbot.MaybeInaccessibleMessage (a callback query's message) satisfies it.
type Message interface {
	Delete(b *gotgbot.Bot, opts *gotgbot.DeleteMessageOpts) (bool, error)
	EditText(b *gotgbot.Bot, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error)
}

// Retire removes an opened message's notification. It tries to delete it
// first. Telegram only lets bots delete their own messages for 48 hours, so if
// that fails, it replaces the notification's text with openedText instead,
// which also removes its Open button. Only the bot's own notification is
// changed; the message content is never involved.
func Retire(b *gotgbot.Bot, msg Message, openedText string) error {
	_, deleteErr := msg.Delete(b, &gotgbot.DeleteMessageOpts{})
	if deleteErr == nil {
		return nil
	}
	// No ReplyMarkup: the edit removes the inline keyboard.
	if _, _, editErr := msg.EditText(b, &gotgbot.EditMessageTextOpts{Text: openedText}); editErr != nil {
		return fmt.Errorf("failed to delete notification (%w) and to edit it (%w)", deleteErr, editErr)
	}
	return nil
}

// SourceGone reports whether copying a message failed because it no longer
// exists: the sender deleted it for both sides, or their account is gone.
func SourceGone(err error) bool {
	var tgErr *gotgbot.TelegramError
	if !errors.As(err, &tgErr) {
		return false
	}
	description := strings.ToLower(tgErr.Description)
	return strings.Contains(description, "message to copy not found") ||
		strings.Contains(description, "chat not found")
}

// MarkUnavailable replaces a notification's text with text (e.g. "the sender
// deleted this message") and removes its Open button, so the outcome stays
// visible in the chat. Bots can edit their own messages without a time limit.
func MarkUnavailable(b *gotgbot.Bot, msg Message, text string) error {
	// No ReplyMarkup: the edit removes the inline keyboard.
	if _, _, err := msg.EditText(b, &gotgbot.EditMessageTextOpts{Text: text}); err != nil {
		return fmt.Errorf("failed to mark notification as unavailable: %w", err)
	}
	return nil
}
