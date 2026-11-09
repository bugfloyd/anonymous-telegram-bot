package common

import (
	"fmt"
	"log"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/telegram-incognito-bot/common/buttons"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/bugfloyd/telegram-incognito-bot/secrets"
)

// signButton signs button data that refers to another user, for the user who
// will see the button (see the buttons package).
func signButton(viewerUUID, data string) string {
	return buttons.Sign(secrets.ButtonKey, viewerUUID, data)
}

// authenticateButton accepts a press only if its data was issued by the bot to
// the person pressing; it then strips the signature so handlers see plain
// data. Rejected presses get a short notice and are otherwise ignored.
func (r *RootHandler) authenticateButton(b *gotgbot.Bot, ctx *ext.Context) (bool, error) {
	cb := ctx.Update.CallbackQuery
	var markup *gotgbot.InlineKeyboardMarkup
	switch m := cb.Message.(type) {
	case gotgbot.Message:
		markup = m.ReplyMarkup
	case *gotgbot.Message:
		markup = m.ReplyMarkup
	}
	data, ok := buttons.Authenticate(secrets.ButtonKey, r.user.UUID, cb.Data, markup)
	if !ok {
		log.Println("rejected button press: data not issued to this user")
		if _, err := cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: i18n.T(i18n.InvalidButtonText)}); err != nil {
			return false, fmt.Errorf("failed to answer callback: %w", err)
		}
		return false, nil
	}
	cb.Data = data
	return true, nil
}
