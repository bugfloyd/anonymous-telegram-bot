package users

import (
	"github.com/bugfloyd/anonymous-telegram-bot/common/i18n"
	"time"
)

type User struct {
	UUID           string `dynamo:",hash"`
	UserID         int64  `index:"UserID-GSI,hash"`
	Username       string `index:"Username-GSI,hash"`
	State          State
	Blacklist      []string      `dynamo:",set,omitempty,omitemptyelem"`
	ContactUUID    string        `dynamo:",omitempty"`
	ReplyMessageID int64         `dynamo:",omitempty"`
	Language       i18n.Language `dynamo:",omitempty"`
	LinkKey        int32         `index:"LinkKey-GSI,hash"`
	CreatedAt      time.Time     `dynamo:",unixtime" index:"LinkKey-GSI,range"`
}

type State string

const (
	Idle            State = "IDLE"
	Sending         State = "SENDING"
	SettingUsername State = "SETTING_USERNAME"
)
