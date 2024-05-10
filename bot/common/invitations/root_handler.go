package invitations

import (
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"
	"github.com/bugfloyd/anonymous-telegram-bot/common/users"
	"strconv"
	"strings"
)

type RootHandler struct {
	user            *users.User
	userRepo        users.UserRepository
	invitationsRepo Repository
}

type Command string
type CallbackCommand string

func NewRootHandler() *RootHandler {
	return &RootHandler{}
}

func (r *RootHandler) init(commandName interface{}) handlers.Response {
	return func(b *gotgbot.Bot, ctx *ext.Context) error {
		return r.runCommand(b, ctx, commandName)
	}
}

func (r *RootHandler) HandleUserAndRepos(ctx *ext.Context) error {
	// create user repo
	userRepo, err := users.NewUserRepository()
	if err != nil {
		return fmt.Errorf("failed to init db repo: %w", err)
	}

	user, err := userRepo.ReadUserByUserId(ctx.EffectiveUser.Id)
	if err != nil {
		user, err = userRepo.CreateUser(ctx.EffectiveUser.Id)
		if err != nil || user == nil {
			return fmt.Errorf("failed to process user: %w", err)
		}
	}

	r.user = user
	r.userRepo = *userRepo

	// create invitations repo
	invitationsRepo, err := NewRepository()
	if err != nil {
		return fmt.Errorf("failed to init invitations db repo: %w", err)
	}
	r.invitationsRepo = *invitationsRepo
	return nil
}

func (r *RootHandler) runCommand(b *gotgbot.Bot, ctx *ext.Context, command interface{}) error {
	err := r.HandleUserAndRepos(ctx)
	if err != nil {
		return err
	}

	switch c := command.(type) {
	case Command:
		switch c {
		case InviteCommand:
			return r.inviteCommandHandler(b, ctx)
		case RegisterCommand:
			return r.registerCommandHandler(b, ctx)
		}
	case CallbackCommand:
		// Reset user state if necessary
		if r.user.State != users.Idle || r.user.ContactUUID != "" || r.user.ReplyMessageID != 0 {
			err := r.userRepo.ResetUserState(r.user)
			if err != nil {
				return err
			}
		}

		switch c {
		case GenerateInvitationCallback:
			return r.manageInvitation(b, ctx, "GENERATE")
		case CancelSendingInvitationCodeCallback:
			return r.invitationCodeCallback(b, ctx, "CANCEL")
		}
	}
	return nil
}

func (r *RootHandler) inviteCommandHandler(b *gotgbot.Bot, ctx *ext.Context) error {
	invitationUser, err := r.invitationsRepo.readUser(r.user.UUID)
	if err != nil && strings.Contains(err.Error(), "dynamo: no item found") {
		_, err = ctx.EffectiveMessage.Reply(b, "You don't have any invitations!", nil)
		if err != nil {
			return fmt.Errorf("failed to reply with user invitations: %w", err)
		}
		return nil
	} else if err != nil {
		return err
	}

	invitations, err := r.invitationsRepo.readInvitationsByUser(r.user.UUID)
	if err != nil {
		return err
	}

	var msg strings.Builder
	var replyMarkup gotgbot.InlineKeyboardMarkup

	if invitationUser.Type == "ZERO" {
		msg.WriteString(fmt.Sprintf("Total invitations available: *%d*\nTotal invitations used: *%d*", invitationUser.InvitationsLeft, invitationUser.InvitationsUsed))

		if len(*invitations) == 0 {
			msg.WriteString("\n\n" + "You have no generated invitation codes\\.")
		} else {
			msg.WriteString("\n\n" + fmt.Sprintf("You have *%d* generated invitation codes:\n", len(*invitations)))

			// Iterate through the slice of invitations and add each to the text
			for _, inv := range *invitations {
				// Create the formatted string for each invitation
				escapedItemID := strings.ReplaceAll(inv.ItemID, "-", "\\-")
				invitationCode := strings.TrimPrefix(escapedItemID, "INVITATION#")
				line := fmt.Sprintf("`%s` %d/%d\n", invitationCode, inv.InvitationsUsed, inv.InvitationsLeft)
				msg.WriteString(line)
			}
		}

		if invitationUser.InvitationsLeft > 0 {
			replyMarkup = gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{
						{
							Text:         "Generate Code",
							CallbackData: "inv|g",
						},
					},
				},
			}
		}
	} else {
		_, err = ctx.EffectiveMessage.Reply(b, "You don't have any invitations!", nil)
		if err != nil {
			return fmt.Errorf("failed to reply with user invitations: %w", err)
		}
		return nil
	}

	_, err = ctx.EffectiveMessage.Reply(b, msg.String(), &gotgbot.SendMessageOpts{
		ReplyMarkup: replyMarkup,
		ParseMode:   gotgbot.ParseModeMarkdownV2,
	})
	if err != nil {
		return fmt.Errorf("failed to reply with user invitations: %w", err)
	}

	return nil
}

func (r *RootHandler) manageInvitation(b *gotgbot.Bot, ctx *ext.Context, action string) error {
	cb := ctx.Update.CallbackQuery
	_, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{})
	if err != nil {
		return fmt.Errorf("failed to update invitation manager message markup: %w", err)
	}

	if action == "GENERATE" {
		inviter, err := r.invitationsRepo.readUser(r.user.UUID)
		if err != nil {
			return err
		}

		if inviter.InvitationsLeft == 0 {
			// Send reply instruction
			_, err = ctx.EffectiveMessage.Reply(b, "You do not have any invitation codes left!", nil)
			if err != nil {
				return fmt.Errorf("failed to send reply message: %w", err)
			}

			// Send callback answer to telegram
			_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
				Text: "No invitations left!",
			})
			if err != nil {
				return fmt.Errorf("failed to answer callback: %w", err)
			}
			return nil
		}

		// Set user status to generating invitation
		err = r.userRepo.UpdateUser(r.user, map[string]interface{}{
			"State":          GeneratingInvitationState,
			"ContactUUID":    "",
			"ReplyMessageID": 0,
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		// Send reply instruction
		_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Please enter the number of available usages for this code\\.\nNote that this number should be lower than your total available invitations which is currently *%d*\\.", inviter.InvitationsLeft), &gotgbot.SendMessageOpts{
			ParseMode: gotgbot.ParseModeMarkdownV2,
		})
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}

		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: "Generating invitation code...",
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}
	}

	return nil
}

func (r *RootHandler) GenerateInvitation(b *gotgbot.Bot, ctx *ext.Context) error {
	invitationCount := ctx.EffectiveMessage.Text

	user, err := r.invitationsRepo.readUser(r.user.UUID)
	if err != nil {
		return err
	}

	// Try to parse the string as an integer
	number, err := strconv.Atoi(invitationCount)
	if err != nil || number < 1 {
		_, err := ctx.EffectiveMessage.Reply(b, "Input is not a valid integer. Enter again.", nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
		return nil
	}
	count := uint32(number)

	// Check if the integer is within the range of uint8
	if count > user.InvitationsLeft {
		_, err := ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Currently you only have *%d* invitations left and cannot generate a code with *%d* usage\\.", user.InvitationsLeft, count), &gotgbot.SendMessageOpts{
			ParseMode: gotgbot.ParseModeMarkdownV2,
		})
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
		return nil
	}

	// Generate a unique invitation code
	code, err := generateUniqueInvitationCode()
	if err != nil {
		return fmt.Errorf("failed to genemrate invitation code: %w", err)
	}
	invitation, err := r.invitationsRepo.createInvitation("whisper-"+code, user.UserUUID, count)
	if err != nil {
		return err
	}

	// Update user
	err = r.invitationsRepo.updateUser(user, map[string]interface{}{
		"InvitationsLeft": user.InvitationsLeft - count,
	})
	if err != nil {
		return err
	}

	_, err = ctx.EffectiveMessage.Reply(b, fmt.Sprintf("Invitation created\\!\n\nCode: `%s`\nUsages left: *%d*", strings.TrimPrefix(invitation.ItemID, "INVITATION#"), invitation.InvitationsLeft), &gotgbot.SendMessageOpts{
		ParseMode: gotgbot.ParseModeMarkdownV2,
	})
	if err != nil {
		return fmt.Errorf("failed to send reply message: %w", err)
	}

	// Reset sender user
	err = r.userRepo.ResetUserState(r.user)
	if err != nil {
		return err
	}

	return nil
}

func (r *RootHandler) registerCommandHandler(b *gotgbot.Bot, ctx *ext.Context) error {
	user, err := r.invitationsRepo.readUser(r.user.UUID)

	if user == nil || err != nil {
		// Set user state to sending invitation code
		err = r.userRepo.UpdateUser(r.user, map[string]interface{}{
			"State":          SendingInvitationCodeState,
			"ContactUUID":    "",
			"ReplyMessageID": 0,
		})
		if err != nil {
			return fmt.Errorf("failed to update user state: %w", err)
		}

		_, err = ctx.EffectiveMessage.Reply(b, "If you have an invitation code please enter it below.", &gotgbot.SendMessageOpts{
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{
						{
							Text:         i18n.T(i18n.CancelButtonText),
							CallbackData: "inv|reg|c",
						},
					},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to reply with user invitations: %w", err)
		}
	} else {
		_, err = ctx.EffectiveMessage.Reply(b, "You are already registered silly!", nil)
		if err != nil {
			return fmt.Errorf("failed to reply with user invitations: %w", err)
		}
	}

	return nil
}

func (r *RootHandler) ValidateCode(b *gotgbot.Bot, ctx *ext.Context) error {
	invitationCode := ctx.EffectiveMessage.Text

	invitation, err := r.invitationsRepo.readInvitation(invitationCode)

	if err != nil || invitation == nil || invitation.InvitationsLeft == 0 {
		// Send username instruction
		_, err = ctx.EffectiveMessage.Reply(b, "The code you entered is not valid or used before. Please check the code and enter again.", &gotgbot.SendMessageOpts{
			ReplyMarkup: gotgbot.InlineKeyboardMarkup{
				InlineKeyboard: [][]gotgbot.InlineKeyboardButton{
					{
						{
							Text:         i18n.T(i18n.CancelButtonText),
							CallbackData: "inv|reg|c",
						},
					},
				},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
		return nil
	} else {
		err := r.invitationsRepo.updateInvitation(invitation, map[string]interface{}{
			"InvitationsLeft": invitation.InvitationsLeft - 1,
			"InvitationsUsed": invitation.InvitationsUsed + 1,
		})
		if err != nil {
			return err
		}

		user, err := r.invitationsRepo.readUser(invitation.UserUUID)

		if err != nil {
			return fmt.Errorf("failed to get inviter user: %w", err)
		}

		err = r.invitationsRepo.updateUser(user, map[string]interface{}{
			"InvitationsUsed": user.InvitationsUsed + 1,
		})
		if err != nil {
			return err
		}

		_, err = r.invitationsRepo.createUser(r.user.UUID)
		if err != nil {
			return fmt.Errorf("failed to create user after registration: %w", err)
		}

		err = r.userRepo.ResetUserState(r.user)
		if err != nil {
			return fmt.Errorf("failed to reset user state: %w", err)
		}

		// Send username instruction
		_, err = ctx.EffectiveMessage.Reply(b, "You registered successfully! Now you can use /link command to get your own link and start using the bot! :)", nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
		return nil
	}
}

func (r *RootHandler) invitationCodeCallback(b *gotgbot.Bot, ctx *ext.Context, action string) error {
	cb := ctx.Update.CallbackQuery

	// Remove invitation code command buttons
	_, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{})
	if err != nil {
		return fmt.Errorf("failed to update invitation code message markup: %w", err)
	}

	if action == "CANCEL" {
		// Send callback answer to telegram
		_, err = cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{
			Text: i18n.T(i18n.NeverMindButtonText),
		})
		if err != nil {
			return fmt.Errorf("failed to answer callback: %w", err)
		}

		// Send instruction
		_, err = ctx.EffectiveMessage.Reply(b, "Okay! You can always use the command /register to enter invitation codes when you get a valid one!", nil)
		if err != nil {
			return fmt.Errorf("failed to send reply message: %w", err)
		}
	} else if action == "SET" {

	}
	return nil
}
