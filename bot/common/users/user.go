package users

import (
	"time"

	"github.com/bugfloyd/telegram-incognito-bot/common/conversation"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
)

// User is stored as one DynamoDB item. The attribute names and types must
// stay compatible with existing data; see TestReadsExistingItemFormat.
type User struct {
	UUID   string `dynamodbav:"UUID"`
	UserID int64  `dynamodbav:"UserID"`
	// Username is a GSI key, and DynamoDB rejects empty strings in index
	// keys: an unset username must be absent from the item, never "".
	Username       string        `dynamodbav:"Username,omitempty"`
	State          State         `dynamodbav:"State"`
	Blacklist      []string      `dynamodbav:"Blacklist,stringset,omitempty"`
	ContactUUID    string        `dynamodbav:"ContactUUID,omitempty"`
	ReplyMessageID int64         `dynamodbav:"ReplyMessageID,omitempty"`
	Language       i18n.Language `dynamodbav:"Language,omitempty"`
	LinkKey        int32         `dynamodbav:"LinkKey"`
	CreatedAt      time.Time     `dynamodbav:"CreatedAt,unixtime"`
	// LastSentAt is when the user's last message in the current conversation
	// was delivered (unix seconds), while State is Continuing.
	LastSentAt int64 `dynamodbav:"LastSentAt,omitempty"`
	// Role is the user's own role in the conversation with ContactUUID, while
	// State is Sending or Continuing: Anonymous if they opened the contact's
	// link, Known if they own the link the contact opened.
	Role conversation.Role `dynamodbav:"Role,omitempty"`

	// Invitations (see the invitations package).
	// Level is how far the user is from an admin in the invitation tree:
	// 1 = invited by an admin, 2 = invited by a level-1 user, and so on.
	// 0 means not registered. Who invited whom is deliberately not stored.
	Level int `dynamodbav:"Level,omitempty"`
	// InvitesGranted are extra invitations given to this user by an admin,
	// on top of their level's allowance.
	InvitesGranted int `dynamodbav:"InvitesGranted,omitempty"`
	// InvitesAllocated is how many invitation uses this user has put into codes.
	InvitesAllocated int `dynamodbav:"InvitesAllocated,omitempty"`
}

// Registered reports whether the user joined with an invitation.
func (u *User) Registered() bool {
	return u.Level > 0
}

type State string

const (
	Idle State = "IDLE"
	// Sending: the user chose a recipient (opened a link or pressed Reply);
	// their next message goes there.
	Sending State = "SENDING"
	// Continuing: a message was delivered; follow-ups still go to the same
	// person until something else interrupts (see the conversation package).
	Continuing      State = "CONTINUING"
	SettingUsername State = "SETTING_USERNAME"
	// EnteringInvitationCode: the user's next message is an invitation code.
	EnteringInvitationCode State = "ENTERING_INVITATION_CODE"
	// CreatingInvitation: the user's next message is the number of uses for a
	// new invitation code.
	CreatingInvitation State = "CREATING_INVITATION"
)
