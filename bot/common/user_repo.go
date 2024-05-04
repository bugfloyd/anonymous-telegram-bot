package common

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/google/uuid"
	"github.com/guregu/dynamo"
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

func (repo *UserRepository) createUser(userId int64) (*User, error) {
	u := User{
		UUID:      uuid.New().String(),
		UserID:    userId,
		State:     Idle,
		LinkKey:   int32(rand.Intn(900000) + 100000),
		CreatedAt: time.Now(),
	}
	err := repo.table.Put(u).Run()
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) readUserByUUID(uuid string) (*User, error) {
	var u User
	err := repo.table.Get("UUID", uuid).One(&u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (repo *UserRepository) readUserByUserId(userId int64) (*User, error) {
	var u User
	err := repo.table.Get("UserID", userId).Index("UserID-GSI").One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) readUserByUsername(username string) (*User, error) {
	var u User
	err := repo.table.Get("Username", username).Index("Username-GSI").One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) readUserByLinkKey(linkKey int32, createdAt int64) (*User, error) {
	var u User
	err := repo.table.Get("LinkKey", linkKey).Index("LinkKey-GSI").Range("CreatedAt", dynamo.Equal, createdAt).One(&u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) updateUser(uuid string, updates map[string]interface{}) error {
	updateBuilder := repo.table.Update("UUID", uuid)
	for key, value := range updates {
		updateBuilder = updateBuilder.Set(key, value)
	}
	err := updateBuilder.Run()
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}
	return nil
}

func (repo *UserRepository) resetUserState(uuid string) error {
	err := repo.updateUser(uuid, map[string]interface{}{
		"State":          Idle,
		"ContactUUID":    nil,
		"ReplyMessageID": nil,
	})
	if err != nil {
		return fmt.Errorf("failed to reset user state: %w", err)
	}
	return nil
}

func (repo *UserRepository) updateBlacklist(uuid string, method string, value string) error {
	updateBuilder := repo.table.Update("UUID", uuid)

	switch method {
	case "add":
		updateBuilder = updateBuilder.AddStringsToSet("Blacklist", value)
	case "delete":
		updateBuilder = updateBuilder.DeleteStringsFromSet("Blacklist", value)
	case "clear":
		updateBuilder = updateBuilder.Set("Blacklist", nil)
	default:
		return fmt.Errorf("invalid method")
	}

	err := updateBuilder.Run()

	if err != nil {
		return fmt.Errorf("failed to %s blacklist: %w", method, err)
	}
	return nil
}
