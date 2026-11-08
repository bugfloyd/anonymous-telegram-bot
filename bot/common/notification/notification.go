// Package notification retires the "you have a new message" notification
// once the recipient has opened the message.
package notification

import (
	"fmt"

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
