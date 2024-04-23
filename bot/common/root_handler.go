package common

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
)

type Command string

const (
	StartCommand           Command = "start"
	InfoCommand            Command = "info"
	LinkCommand            Command = "link"
	UsernameCommand        Command = "username"
	LanguageCommand        Command = "language"
	TextMessage            Command = "text"
	ReplyCallback          Command = "reply-callback"
	BlockCallback          Command = "block-callback"
	OpenCallback           Command = "open-callback"
	SetUsernameCallback    Command = "set-username-callback"
	RemoveUsernameCallback Command = "remove-username-callback"
	CancelUsernameCallback Command = "cancel-username-callback"
	SetLanguageCallback    Command = "set-language-callback"
	CancelLanguageCallback Command = "cancel-language-callback"
)

type RootHandler struct {
	user     User
	userRepo UserRepository
}

func NewRootHandler() *RootHandler {
	return &RootHandler{}
}

func (r *RootHandler) init(commandName Command) handlers.Response {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		return r.runCommand(b, ctx, commandName)
	}
}

func (r *RootHandler) runCommand(b *gotgbot.Bot, ctx *ext.Context, command Command) error {
	// create user repo
	userRepo, err := NewUserRepository()
	if err != nil {
		return fmt.Errorf("failed to init db repo: %w", err)
	}
	user, err := r.processUser(userRepo, ctx)

	if err != nil || user == nil {
		return fmt.Errorf("failed to process user: %w", err)
	}
	r.user = *user
	r.userRepo = *userRepo

	// Load locale
	var language i18n.Language
	if user.Language != "" {
		language = user.Language
	} else {
		if ctx.EffectiveUser.LanguageCode == "fa" {
			language = i18n.FaIR
		} else if ctx.EffectiveUser.LanguageCode == "en" {
			language = i18n.EnUS
		} else {
			language = i18n.EnUS
		}
	}
	i18n.SetLocale(language)

	// Decide which function to call based on the command
	switch command {
	case StartCommand:
		return r.start(b, ctx)
	case InfoCommand:
		return r.info(b, ctx)
	case LinkCommand:
		return r.getLink(b, ctx)
	case UsernameCommand:
		return r.manageUsername(b, ctx)
	case LanguageCommand:
		return r.manageLanguage(b, ctx)
	case TextMessage:
		return r.processText(b, ctx)
	case ReplyCallback:
		return r.replyCallback(b, ctx)
	case BlockCallback:
		return r.blockCallback(b, ctx)
	case OpenCallback:
		return r.openCallback(b, ctx)
	case SetUsernameCallback:
		return r.usernameCallback(b, ctx, "SET")
	case RemoveUsernameCallback:
		return r.usernameCallback(b, ctx, "REMOVE")
	case CancelUsernameCallback:
		return r.usernameCallback(b, ctx, "CANCEL")
	case SetLanguageCallback:
		return r.languageCallback(b, ctx, "SET")
	case CancelLanguageCallback:
		return r.languageCallback(b, ctx, "CANCEL")
	default:
		return fmt.Errorf("unknown command: %s", command)
	}
}

func (r *RootHandler) processUser(userRepo *UserRepository, ctx *ext.Context) (*User, error) {
	user, err := userRepo.readUserByUserId(ctx.EffectiveUser.Id)
	if err != nil {
		user, err = userRepo.createUser(ctx.EffectiveUser.Id)
		if err != nil {
			return nil, err
		}
	}

	return user, nil
}

func (r *RootHandler) start(b *gotgbot.Bot, ctx *ext.Context) error {
	args := ctx.Args()
	if len(args) == 1 && args[0] == "/start" {
		// Reset user state
		err := r.userRepo.resetUserState(r.user.UUID)
		if err != nil {
			return err
		}

		_, err = b.SendMessage(ctx.EffectiveChat.Id, i18n.T(i18n.StartMessage), &gotgbot.SendMessageOpts{})
		if err != nil {
			return fmt.Errorf("failed to send bot info: %w", err)
		}
		return nil
	}
	if len(args) == 2 && args[0] == "/start" {

		var err error
		var receiverUser *User
		var identity string

		if strings.HasPrefix(args[1], "_") {
			username := args[1][1:]
			receiverUser, err = r.userRepo.readUserByUsername(username)
		} else {
			receiverUser, err = r.userRepo.readUserByUUID(args[1])
		}

		if err != nil || receiverUser == nil {
			_, err = b.SendMessage(ctx.EffectiveChat.Id, "User not found! Wrong link?", &gotgbot.SendMessageOpts{})
			if err != nil {
				return fmt.Errorf("failed to send bot info: %w", err)
			}
			return nil
		}

		if receiverUser.UUID == r.user.UUID {
			_, err = b.SendMessage(ctx.EffectiveChat.Id, "Do you really want to talk to yourself? So sad! Share your link with friends or post it on social media to get anonymous messages!", &gotgbot.SendMessageOpts{})
			if err != nil {
				return fmt.Errorf("failed to send bot info: %w", err)
			}
			return nil
		}

		// Set user state to sending
		err = r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":       Sending,
			"ContactUUID": receiverUser.UUID,
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		if receiverUser.Name != "" {
			identity = receiverUser.Name
		} else if receiverUser.Username != "" {
			identity = receiverUser.Username
		} else {
			identity = receiverUser.UUID
		}

		_, err = b.SendMessage(ctx.EffectiveChat.Id, fmt.Sprintf(i18n.T(i18n.InitialSendMessagePrompt), identity), &gotgbot.SendMessageOpts{})
		if err != nil {
			return fmt.Errorf("failed to send bot info: %w", err)
		}
	}

	return nil
}

func (r *RootHandler) info(b *gotgbot.Bot, ctx *ext.Context) error {
	_, err := b.SendMessage(ctx.EffectiveChat.Id, "Bugfloyd Anonymous bot\n\nSource code:\nhttps://github.com/bugfloyd/anonymous-telegram-bot", &gotgbot.SendMessageOpts{})
	if err != nil {
		return fmt.Errorf("failed to send bot info: %w", err)
	}
	err = r.userRepo.resetUserState(r.user.UUID)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) getLink(b *gotgbot.Bot, ctx *ext.Context) error {
	var link string
	if r.user.Username != "" {
		usernameLink := fmt.Sprintf("https://t.me/%s?start=_%s", b.User.Username, r.user.Username)
		uuidLink := fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, r.user.UUID)
		link = fmt.Sprintf("%s\n\nor:\n\n%s", usernameLink, uuidLink)
	} else {
		link = fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, r.user.UUID)
	}
	_, err := ctx.EffectiveMessage.Reply(b, link, nil)
	if err != nil {
		return err
	}
	err = r.userRepo.resetUserState(r.user.UUID)
	if err != nil {
		return err
	}
	return nil
}

func (r *RootHandler) processText(b *gotgbot.Bot, ctx *ext.Context) error {
	switch r.user.State {
	case Sending:
		return r.sendAnonymousMessage(b, ctx)
	case SettingUsername:
		return r.setUsername(b, ctx)
	default:
		return r.sendError(b, ctx, "Unknown Command!")
	}
}

func (r *RootHandler) sendError(b *gotgbot.Bot, ctx *ext.Context, message string) error {
	errorMessage := fmt.Sprintf("Error: %s", message)
	_, err := ctx.EffectiveMessage.Reply(b, errorMessage, nil)
	if err != nil {
		return fmt.Errorf("failed to send error message: %w", err)
	}
	return nil
}

func (r *RootHandler) sendAnonymousMessage(b *gotgbot.Bot, ctx *ext.Context) error {
	receiver, err := r.userRepo.readUserByUUID(r.user.ContactUUID)
	if err != nil {
		return fmt.Errorf("failed to get receiver: %w", err)
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
						CallbackData: fmt.Sprintf("b|%s", sender.UUID),
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

// calback for block
func (r *RootHandler) blockCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery
	split := strings.Split(cb.Data, "|")
	if len(split) != 2 {
		return fmt.Errorf("invalid callback data: %s", cb.Data)
	}
	receiverUUID := split[1]

	// create a hash of receiverUUID
	hashedUUID := sha256.Sum256([]byte(receiverUUID))

	// Block the user, add hash of uuid to Blacklist of the user
	err := r.userRepo.updateBlacklist(r.user.UUID, "append", string(hashedUUID[:]))

	if err != nil {
		return fmt.Errorf("failed to block user: %w", err)
	}

	// Send callback answer to telegram
	_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
		Text: "User blocked!",
	})
	if err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}

	// Update button to unblock
	_, _, err = cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "Unblock",
						CallbackData: fmt.Sprintf("ub|%s", receiverUUID),
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

func (r *RootHandler) manageUsername(b *gotgbot.Bot, ctx *ext.Context) error {
	var text string
	var buttons [][]gotgbot.InlineKeyboardButton

	if r.user.Username != "" {
		text = fmt.Sprintf("Your current username is: %s", r.user.Username)
		buttons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         "Change",
					CallbackData: "u",
				},
				{
					Text:         "Remove",
					CallbackData: "ru",
				},
				{
					Text:         "Cancel",
					CallbackData: "cu",
				},
			},
		}
	} else {
		text = "You don't have a username!"
		buttons = [][]gotgbot.InlineKeyboardButton{
			{
				{
					Text:         "Set one",
					CallbackData: "u",
				},
				{
					Text:         "Cancel",
					CallbackData: "cu",
				},
			},
		}
	}

	_, err := b.SendMessage(ctx.EffectiveChat.Id, text, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: buttons,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send username info: %w", err)
	}

	return nil
}

func (r *RootHandler) usernameCallback(b *gotgbot.Bot, ctx *ext.Context, action string) error {
	cb := ctx.Update.CallbackQuery

	// Remove username command buttons
	_, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{})
	if err != nil {
		return fmt.Errorf("failed to update username message markup: %w", err)
	}

	if action == "CANCEL" {
		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Never mind!",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
		// Reset sender user
		err = r.userRepo.resetUserState(r.user.UUID)
		if err != nil {
			return err
		}
	} else if action == "SET" {
		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":          SettingUsername,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		// Send reply instruction
		_, err = ctx.EffectiveMessage.Reply(b, "Create a username that starts with a letter, includes 3-20 characters, and may contain letters, numbers, or underscores (_). Usernames are automatically converted to lowercase. \n\nEnter new username:", nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Setting username...",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	} else if action == "REMOVE" {
		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":          Idle,
			"Username":       nil,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
		})
		if err != nil {
			return fmt.Errorf("failed to remove username: %w", err)
		}

		_, _, err = cb.Message.EditText(b, "Username has been removed!", &gotgbot.EditMessageTextOpts{})
		if err != nil {
			return fmt.Errorf("failed to update username message text: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Username removed!",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	}

	return nil
}

func (r *RootHandler) setUsername(b *gotgbot.Bot, ctx *ext.Context) error {
	username := ctx.EffectiveMessage.Text

	if isValidUsername(username) == false {
		// Send username instruction
		_, err := ctx.EffectiveMessage.Reply(b, "The entered username is not valid. Enter another one:", nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
		return nil
	}

	// Convert to lowercase
	username = strings.ToLower(username)

	existingUser, err := r.userRepo.readUserByUsername(username)
	if err != nil || existingUser == nil {
		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"Username":       username,
			"State":          Idle,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
		})
		if err != nil {
			return fmt.Errorf("failed to update username: %w", err)
		}

		// Send username instruction
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Username has been set: %s", username), nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
	} else {
		var text string
		if existingUser.UUID != r.user.UUID {
			text = "The entered username exists. Enter another one:"
		} else {
			text = "You already own this username silly! If you want to change it, run the username command once more!"

			// Reset sender user
			err = r.userRepo.resetUserState(r.user.UUID)
			if err != nil {
				return err
			}
		}
		// Send username instruction
		_, err = ctx.EffectiveMessage.Reply(b, text, nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
	}

	return nil
}

func isValidUsername(username string) bool {
	// Check length
	if len(username) < 3 || len(username) > 20 {
		return false
	}

	// Regular expression to check valid characters
	// ^[a-zA-Z0-9_]+$
	// This checks the string consists only of English letters, digits, and underscores
	re := regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !re.MatchString(username) {
		return false
	}

	// Check first character (not a digit or underscore)
	firstChar := username[0]
	if firstChar == '_' || ('0' <= firstChar && firstChar <= '9') {
		return false
	}

	return true
}

func (r *RootHandler) manageLanguage(b *gotgbot.Bot, ctx *ext.Context) error {
	var text string
	if r.user.Language != "" {
		text = fmt.Sprintf("Your language is: %s", r.user.Language)
	} else {
		text = "You don't have a preferred language yet."
	}
	_, err := b.SendMessage(ctx.EffectiveChat.Id, text, &gotgbot.SendMessageOpts{
		ReplyMarkup: gotgbot.InlineKeyboardMarkup{
			InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
				{
					{
						Text:         "English",
						CallbackData: fmt.Sprintf("l|%s", i18n.EnUS),
					},
					{
						Text:         "Farsi",
						CallbackData: fmt.Sprintf("l|%s", i18n.FaIR),
					},
					{
						Text:         "Cancel",
						CallbackData: "lc",
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to send lkanguage info: %w", err)
	}

	return nil
}

func (r *RootHandler) languageCallback(b *gotgbot.Bot, ctx *ext.Context, action string) error {
	cb := ctx.Update.CallbackQuery

	// Remove language command buttons
	_, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{})
	if err != nil {
		return fmt.Errorf("failed to update language message markup: %w", err)
	}

	if action == "CANCEL" {
		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Never mind!",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
		// Reset sender user
		err = r.userRepo.resetUserState(r.user.UUID)
		if err != nil {
			return err
		}
	} else if action == "SET" {
		split := strings.Split(cb.Data, "|")
		if len(split) != 2 {
			return fmt.Errorf("invalid callback data: %s", cb.Data)
		}

		var isLanguageValid bool = false
		for _, lang := range []i18n.Language{i18n.EnUS, i18n.FaIR} {
			if split[1] == string(lang) {
				isLanguageValid = true
				break
			}
		}

		if isLanguageValid == false {
			return fmt.Errorf("invalid language code in callback data: %s", cb.Data)
		}

		err := r.userRepo.updateUser(r.user.UUID, map[string]interface{}{
			"State":          Idle,
			"ContactUUID":    nil,
			"ReplyMessageID": nil,
			"Language":       split[1],
		})
		if err != nil {
			return fmt.Errorf("failed to update user language: %w", err)
		}

		// Send update status
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Language updated successfully to %s", split[1]), nil)
		if err != nil {
			return fmt.Errorf("failed to send language update message: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Language updated!",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	}
	return nil
}
