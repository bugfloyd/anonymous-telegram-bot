package anonymous

type User struct {
	UUID              string `dynamo:",hash"`
	UserID            int64  `index:"UserID-GSI,hash"`
	State             State
	Name              string
	Blacklist         []string `dynamo:",set,omitempty"`
	ContactUUID       string   `dynamo:",omitempty"`
	ReplyMessageID    int64    `dynamo:",omitempty"`
	DeliveryMessageID int64    `dynamo:",omitempty"`
}

type State string

const (
	REGISTERED State = "REGISTERED"
	SENDING    State = "SENDING"
)
