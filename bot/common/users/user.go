package users

import (
	"time"

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
)
