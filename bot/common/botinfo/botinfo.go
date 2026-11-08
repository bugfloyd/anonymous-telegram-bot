// Package botinfo builds the bot's self-description for the /info command.
package botinfo

import (
	"fmt"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

// SourceCodeURL is where the bot's source code is published.
const SourceCodeURL = "https://github.com/bugfloyd/telegram-incognito-bot"

// Text describes the bot using the name and username Telegram reports for it
// (getMe). It always matches the bot's BotFather settings, and no
// deployment-specific name is hard-coded.
func Text(bot gotgbot.User) string {
	name := bot.FirstName
	if bot.LastName != "" {
		name += " " + bot.LastName
	}
	if bot.Username != "" {
		name += " (@" + bot.Username + ")"
	}
	return fmt.Sprintf("%s\n\nSource code:\n%s", name, SourceCodeURL)
}
