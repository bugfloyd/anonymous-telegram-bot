package common

import (
	"fmt"
	"log"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/telegram-incognito-bot/common/buttons"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/bugfloyd/telegram-incognito-bot/common/labels"
	"github.com/bugfloyd/telegram-incognito-bot/secrets"
)

// Keys for button data and labels, from the identity key.
var (
	buttonKeys *buttons.Keys
	labelKey   labels.Key
)

func init() {
	var err error
	if buttonKeys, err = buttons.NewKeys(secrets.IdentityKey); err != nil {
		log.Fatalf("Failed to create button keys: %v", err)
	}
	if labelKey, err = labels.NewKey(secrets.IdentityKey); err != nil {
		log.Fatalf("Failed to create label key: %v", err)
	}
}

// userButtons are the buttons that refer to another user, by callback. Their
// data is sealed (see the buttons package), and their handlers only get the
// opened Ref.
var userButtons = map[CallbackCommand]buttons.Action{
	OpenCallback:    buttons.OpenMessage,
	ReplyCallback:   buttons.Reply,
	BlockCallback:   buttons.Block,
	UnBlockCallback: buttons.Unblock,
}

// openButton accepts a press only if its data was sealed by the bot for the
// person pressing it, and returns what the button refers to. Rejected presses
// get a short notice and are otherwise ignored.
func (r *RootHandler) openButton(b *gotgbot.Bot, ctx *ext.Context, action buttons.Action) (buttons.Ref, bool, error) {
	cb := ctx.Update.CallbackQuery
	ref, err := buttonKeys.Open(action, r.user.UUID, cb.Data)
	if err != nil {
		log.Println("rejected button press: data not issued to this user")
		if _, err := cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: i18n.T(i18n.InvalidButtonText)}); err != nil {
			return buttons.Ref{}, false, fmt.Errorf("failed to answer callback: %w", err)
		}
		return buttons.Ref{}, false, nil
	}
	return ref, true, nil
}

// button returns a button for viewerUUID that refers to ref.
func button(text string, action buttons.Action, viewerUUID string, ref buttons.Ref) (gotgbot.InlineKeyboardButton, error) {
	data, err := buttonKeys.Seal(action, viewerUUID, ref)
	if err != nil {
		return gotgbot.InlineKeyboardButton{}, fmt.Errorf("failed to seal %s button: %w", action, err)
	}
	return gotgbot.InlineKeyboardButton{Text: text, CallbackData: data}, nil
}

// labelOf is how viewerUUID sees the sender of ref's message, ready to put in
// a text or a button.
func labelOf(viewerUUID string, ref buttons.Ref) string {
	return labels.Embed(labelKey.For(viewerUUID, ref.Other, ref.Role))
}

// withLabel adds a label to a button text, e.g. "Reply · 🦊 K3F9".
func withLabel(text, label string) string {
	return text + " · " + label
}

// openKeyboard is the keyboard of the notification about ref's message:
// Open, in the receiver's language.
func openKeyboard(receiverUUID string, language i18n.Language, ref buttons.Ref) (gotgbot.InlineKeyboardMarkup, error) {
	open, err := button(i18n.TT(i18n.OpenMessageButtonText, language), buttons.OpenMessage, receiverUUID, ref)
	if err != nil {
		return gotgbot.InlineKeyboardMarkup{}, err
	}
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{open}}}, nil
}

// messageKeyboard is the keyboard under an opened message: Reply, showing the
// sender's label, and Block.
func messageKeyboard(viewerUUID string, ref buttons.Ref) (gotgbot.InlineKeyboardMarkup, error) {
	reply, err := button(withLabel(i18n.T(i18n.ReplyButtonText), labelOf(viewerUUID, ref)), buttons.Reply, viewerUUID, ref)
	if err != nil {
		return gotgbot.InlineKeyboardMarkup{}, err
	}
	block, err := button(i18n.T(i18n.BlockButtonText), buttons.Block, viewerUUID, ref)
	if err != nil {
		return gotgbot.InlineKeyboardMarkup{}, err
	}
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{reply, block}}}, nil
}

// blockedKeyboard replaces messageKeyboard after blocking: Unblock, showing
// the sender's label.
func blockedKeyboard(viewerUUID string, ref buttons.Ref) (gotgbot.InlineKeyboardMarkup, error) {
	unblock, err := button(withLabel(i18n.T(i18n.UnblockButtonText), labelOf(viewerUUID, ref)), buttons.Unblock, viewerUUID, ref)
	if err != nil {
		return gotgbot.InlineKeyboardMarkup{}, err
	}
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{unblock}}}, nil
}
