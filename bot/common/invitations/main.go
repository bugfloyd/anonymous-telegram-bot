package invitations

import (
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers"
	"github.com/PaulSonOfLars/gotgbot/v2/ext/handlers/filters/callbackquery"
	"github.com/bugfloyd/anonymous-telegram-bot/common/users"
)

type User struct {
	ItemID          string `dynamo:",hash" index:"UserUUID-GSI,range"`
	UserUUID        string `index:"UserUUID-GSI,hash"`
	InvitationsLeft uint32
	InvitationsUsed uint32
	Type            string
}

type Invitation struct {
	ItemID          string `dynamo:",hash" index:"UserUUID-GSI,range"`
	UserUUID        string `index:"UserUUID-GSI,hash"`
	InvitationsLeft uint32
	InvitationsUsed uint32
}

const (
	GeneratingInvitationState  users.State = "GENERATING_INVITATION"
	SendingInvitationCodeState users.State = "SENDING_INVITATION_CODE"
)

const (
	InviteCommand   Command = "invite"
	RegisterCommand Command = "register"
)

const (
	GenerateInvitationCallback          CallbackCommand = "generate-invitation-callback"
	CancelSendingInvitationCodeCallback CallbackCommand = "cancel-invitation-code-callback"
)

func InitInvitations(dispatcher *ext.Dispatcher) {
	rootHandler := NewRootHandler()

	dispatcher.AddHandler(handlers.NewCommand(string(InviteCommand), rootHandler.init(InviteCommand)))
	dispatcher.AddHandler(handlers.NewCommand(string(RegisterCommand), rootHandler.init(RegisterCommand)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("inv|g"), rootHandler.init(GenerateInvitationCallback)))
	dispatcher.AddHandler(handlers.NewCallback(callbackquery.Prefix("inv|reg|c"), rootHandler.init(CancelSendingInvitationCodeCallback)))

}
