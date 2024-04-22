package common

import "github.com/bugfloyd/anonymous-telegram-bot/common/i18n"

type User struct {
	UUID           string `dynamo:",hash"`
	UserID         int64  `index:"UserID-GSI,hash"`
	Username       string `index:"Username-GSI,hash"`
	State          State
	Name           string
	Blacklist      []string      `dynamo:",set,omitempty"`
	ContactUUID    string        `dynamo:",omitempty"`
	ReplyMessageID int64         `dynamo:",omitempty"`
	Language       i18n.Language `dynamo:",omitempty"`
}

type State string

const (
	Idle            State = "IDLE"
	Sending         State = "SENDING"
	SettingUsername State = "SETTING_USERNAME"
)
