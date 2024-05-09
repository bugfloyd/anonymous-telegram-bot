package invitations

import (
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/bugfloyd/anonymous-telegram-bot/common/users"
)

type User struct {
	ItemID          string `dynamo:",hash" index:"UserID-GSI,range"`
	UserID          string `index:"UserID-GSI,hash"`
	InvitationsLeft uint32
	InvitationsUsed uint32
	Type            string
}

type Invitation struct {
	ItemID          string `dynamo:",hash" index:"UserID-GSI,range"`
	UserID          string `index:"UserID-GSI,hash"`
	InvitationsLeft uint32
	InvitationsUsed uint32
}

const (
	GeneratingInvitationState users.State = "GENERATING_INVITATION"
)

const (
	InviteCommand Command = "invite"
)

const (
	GenerateInvitationCallback CallbackCommand = "generate-invitation-callback"
)

func InitInvitations(dispatcher *ext.Dispatcher) {
	rootHandler := NewRootHandler()

	dispatcher.AddHandler(handlers.NewCommand(string(InviteCommand), rootHandler.init(InviteCommand)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("inv|g"), rootHandler.init(GenerateInvitationCallback)))
}
