package common

import (
	"fmt"
	"math/rand"
	"os"
	"reflect"
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

func (repo *UserRepository) updateUser(user *User, updates map[string]interface{}) error {
	updateBuilder := repo.table.Update("UUID", user.UUID)
	for key, value := range updates {
		updateBuilder = updateBuilder.Set(key, value)
	}
	err := updateBuilder.Run()
	if err != nil {
		return fmt.Errorf("failed to update user: %w", err)
	}

	// Reflecting on user to update fields based on updates map
	val := reflect.ValueOf(user).Elem() // We use .Elem() to dereference the pointer to user
	for key, value := range updates {
		fieldVal := val.FieldByName(key)
		if fieldVal.IsValid() && fieldVal.CanSet() {
			// Ensure the value is of the correct type
			correctTypeValue := reflect.ValueOf(value)
			if correctTypeValue.Type().ConvertibleTo(fieldVal.Type()) {
				correctTypeValue = correctTypeValue.Convert(fieldVal.Type())
			}
			fieldVal.Set(correctTypeValue)
		}
	}

	return nil
}

func (repo *UserRepository) resetUserState(user *User) error {
	err := repo.updateUser(user, map[string]interface{}{
		"State":          Idle,
		"ContactUUID":    "",
		"ReplyMessageID": 0,
	})
	if err != nil {
		return fmt.Errorf("failed to reset user state: %w", err)
	}
	return nil
}

func (repo *UserRepository) updateBlacklist(user *User, method string, value string) error {
	updateBuilder := repo.table.Update("UUID", user.UUID)

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

	// Update the in-memory user data
	switch method {
	case "add":
		// Ensure the value is not already in the blacklist to avoid duplicates
		if !contains(user.Blacklist, value) {
			user.Blacklist = append(user.Blacklist, value)
		}
	case "delete":
		// Remove the value from the slice
		user.Blacklist = removeFromSlice(user.Blacklist, value)
	case "clear":
		// Clear the slice
		user.Blacklist = []string{}
	}

	return nil
}

// Utility function to check if a slice contains a string
func contains(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}

// Utility function to remove an element from a slice
func removeFromSlice(slice []string, value string) []string {
	newSlice := make([]string, 0)
	for _, item := range slice {
		if item != value {
			newSlice = append(newSlice, item)
		}
	}
	return newSlice
}
