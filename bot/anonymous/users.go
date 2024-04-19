package anonymous

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/google/uuid"
	"github.com/guregu/dynamo"
)

type User struct {
	UUID           string `dynamo:",hash"`
	UserID         int64  `index:"UserID-GSI,hash"`
	State          State
	Name           string
	Blacklist      []string `dynamo:",set,omitempty"`
	ContactUUID    string   `dynamo:",omitempty"`
	ReplyMessageID int64    `dynamo:",omitempty"`
}

type State string

const (
	REGISTERED State = "REGISTERED"
	SENDING    State = "SENDING"
)

type UserRepository struct {
	table dynamo.Table
}

func NewUserRepository() (*UserRepository, error) {
	var sess *session.Session
	customDynamoDbEndpoint := os.Getenv("DYNAMODB_ENDPOINT")
	awsRegion := os.Getenv("AWS_REGION")

	if customDynamoDbEndpoint != "" {
		sess = session.Must(session.NewSession(&aws.Config{
			Region:   aws.String(awsRegion),
			Endpoint: aws.String(customDynamoDbEndpoint),
		}))
	} else {
		sess = session.Must(session.NewSession(&aws.Config{Region: aws.String(awsRegion)}))
	}

	db := dynamo.New(sess)

	return &UserRepository{
		table: db.Table("AnonymousBot"),
	}, nil
}

func (repo *UserRepository) GetUserByUUID(uuid string) (*User, error) {
	var u User
	err := repo.table.Get("UUID", uuid).One(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (repo *UserRepository) GetUserByUserId(userId int64) (*User, error) {
	var u User
	err := repo.table.Get("UserID", userId).Index("UserID-GSI").One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) SetUser(userId int64) (*User, error) {
	u := User{
		UUID:   uuid.New().String(),
		UserID: userId,
		State:  REGISTERED,
	}
	err := repo.table.Put(u).Run()
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return &u, nil
}

func (u *User) SetStateToSending(repo *UserRepository, contactUUID string) error {
	u.State = SENDING
	u.ContactUUID = contactUUID
	err := repo.table.Put(u).Run()
	if err != nil {
		return fmt.Errorf("failed to update user state: %w", err)
	}
	return nil
}

func (u *User) SetStateToReply(repo *UserRepository, contactUUID string, messageID int64) error {
	u.State = SENDING
	u.ContactUUID = contactUUID
	u.ReplyMessageID = messageID
	err := repo.table.Put(u).Run()
	if err != nil {
		return fmt.Errorf("failed to update user state: %w", err)
	}
	return nil
}

func (u *User) SetState(repo *UserRepository, state State) error {
	u.State = state
	err := repo.table.Put(u).Run()
	if err != nil {
		return fmt.Errorf("failed to update user state: %w", err)
	}
	return nil
}
