package users

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/google/uuid"
	"github.com/guregu/dynamo/v2"
)

// TableName must match the table in infra/main.tf.
const TableName = "IncognitoChatBot"

// opTimeout bounds every DynamoDB call so a slow request can't hang the handler.
const opTimeout = 5 * time.Second

type UserRepository struct {
	table dynamo.Table
}

// loadDB creates the DynamoDB client once per process (Lambda execution
// environment) and reuses it for every update. DYNAMODB_ENDPOINT points it
// at a local DynamoDB for development and tests.
var loadDB = sync.OnceValues(func() (*dynamo.DB, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	var opts []func(*dynamodb.Options)
	if endpoint := os.Getenv("DYNAMODB_ENDPOINT"); endpoint != "" {
		opts = append(opts, func(o *dynamodb.Options) { o.BaseEndpoint = aws.String(endpoint) })
	}
	return dynamo.New(cfg, opts...), nil
})

func NewUserRepository() (*UserRepository, error) {
	return newUserRepository(TableName)
}

func newUserRepository(tableName string) (*UserRepository, error) {
	db, err := loadDB()
	if err != nil {
		return nil, err
	}
	return &UserRepository{table: db.Table(tableName)}, nil
}

func opContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

func (repo *UserRepository) CreateUser(userId int64) (*User, error) {
	u := User{
		UUID:      uuid.New().String(),
		UserID:    userId,
		State:     Idle,
		LinkKey:   int32(rand.Intn(900000) + 100000),
		CreatedAt: time.Now(),
	}
	ctx, cancel := opContext()
	defer cancel()
	err := repo.table.Put(u).Run(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) ReadUserByUUID(uuid string) (*User, error) {
	var u User
	ctx, cancel := opContext()
	defer cancel()
	err := repo.table.Get("UUID", uuid).One(ctx, &u)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (repo *UserRepository) ReadUserByUserId(userId int64) (*User, error) {
	var u User
	ctx, cancel := opContext()
	defer cancel()
	err := repo.table.Get("UserID", userId).Index("UserID-GSI").One(ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) ReadUserByUsername(username string) (*User, error) {
	var u User
	ctx, cancel := opContext()
	defer cancel()
	err := repo.table.Get("Username", username).Index("Username-GSI").One(ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) ReadUserByLinkKey(linkKey int32, createdAt int64) (*User, error) {
	var u User
	ctx, cancel := opContext()
	defer cancel()
	err := repo.table.Get("LinkKey", linkKey).Index("LinkKey-GSI").Range("CreatedAt", dynamo.Equal, createdAt).One(ctx, &u)
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) UpdateUser(user *User, updates map[string]interface{}) error {
	updateBuilder := repo.table.Update("UUID", user.UUID)
	for key, value := range updates {
		updateBuilder = updateBuilder.Set(key, value)
	}
	ctx, cancel := opContext()
	defer cancel()
	err := updateBuilder.Run(ctx)
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

func (repo *UserRepository) ResetUserState(user *User) error {
	err := repo.UpdateUser(user, map[string]interface{}{
		"State":          Idle,
		"ContactUUID":    "",
		"ReplyMessageID": 0,
	})
	if err != nil {
		return fmt.Errorf("failed to reset user state: %w", err)
	}
	return nil
}

func (repo *UserRepository) UpdateBlacklist(user *User, method string, value string) error {
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

	ctx, cancel := opContext()
	defer cancel()
	err := updateBuilder.Run(ctx)

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
