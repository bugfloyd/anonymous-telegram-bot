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
}

type State string

const (
	Idle            State = "IDLE"
	Sending         State = "SENDING"
	SettingUsername State = "SETTING_USERNAME"
)
