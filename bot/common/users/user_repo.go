package users

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/google/uuid"
)

// TableName must match the table in infra/main.tf.
const TableName = "IncognitoChatBot"

// Global secondary indexes, as defined in infra/main.tf.
const (
	userIDIndex   = "UserID-GSI"
	usernameIndex = "Username-GSI"
	linkKeyIndex  = "LinkKey-GSI"
)

// opTimeout bounds every DynamoDB call so a slow request can't hang the handler.
const opTimeout = 5 * time.Second

// ErrNotFound is returned when no user matches a lookup.
var ErrNotFound = errors.New("user not found")

type UserRepository struct {
	client *dynamodb.Client
	table  string
}

// loadClient creates the DynamoDB client once per process (Lambda execution
// environment) and reuses it for every update. DYNAMODB_ENDPOINT points it
// at a local DynamoDB for development and tests.
var loadClient = sync.OnceValues(func() (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if endpoint := os.Getenv("DYNAMODB_ENDPOINT"); endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	}), nil
})

func NewUserRepository() (*UserRepository, error) {
	return newUserRepository(TableName)
}

func newUserRepository(table string) (*UserRepository, error) {
	client, err := loadClient()
	if err != nil {
		return nil, err
	}
	return &UserRepository{client: client, table: table}, nil
}

func opContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), opTimeout)
}

func itemKey(userUUID string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"UUID": &types.AttributeValueMemberS{Value: userUUID}}
}

// ---------------------------------------------------------------------------
// Create and read
// ---------------------------------------------------------------------------

// CreateUser stores a new idle user for a Telegram user ID.
func (repo *UserRepository) CreateUser(userID int64) (*User, error) {
	u := User{
		UUID:   uuid.NewString(),
		UserID: userID,
		State:  Idle,
		// Six-digit key; with CreatedAt it identifies the user's share link.
		LinkKey: int32(rand.IntN(900000) + 100000),
		// Stored with second precision, so keep the in-memory copy identical.
		CreatedAt: time.Unix(time.Now().Unix(), 0),
	}
	item, err := attributevalue.MarshalMap(u)
	if err != nil {
		return nil, fmt.Errorf("failed to encode user: %w", err)
	}

	ctx, cancel := opContext()
	defer cancel()
	_, err = repo.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                &repo.table,
		Item:                     item,
		ConditionExpression:      aws.String("attribute_not_exists(#uuid)"),
		ExpressionAttributeNames: map[string]string{"#uuid": "UUID"},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create user: %w", err)
	}
	return &u, nil
}

func (repo *UserRepository) ReadUserByUUID(userUUID string) (*User, error) {
	if userUUID == "" {
		return nil, ErrNotFound
	}
	ctx, cancel := opContext()
	defer cancel()
	out, err := repo.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &repo.table,
		Key:       itemKey(userUUID),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	if out.Item == nil {
		return nil, ErrNotFound
	}
	return unmarshalUser(out.Item)
}

func (repo *UserRepository) ReadUserByUserId(userID int64) (*User, error) {
	return repo.queryOne(userIDIndex, expression.Key("UserID").Equal(expression.Value(userID)))
}

func (repo *UserRepository) ReadUserByUsername(username string) (*User, error) {
	if username == "" { // not a valid index key, and no user has an empty username
		return nil, ErrNotFound
	}
	return repo.queryOne(usernameIndex, expression.Key("Username").Equal(expression.Value(username)))
}

func (repo *UserRepository) ReadUserByLinkKey(linkKey int32, createdAt int64) (*User, error) {
	return repo.queryOne(linkKeyIndex, expression.Key("LinkKey").Equal(expression.Value(linkKey)).
		And(expression.Key("CreatedAt").Equal(expression.Value(createdAt))))
}

// queryOne returns the first user matching key in a global secondary index.
func (repo *UserRepository) queryOne(index string, key expression.KeyConditionBuilder) (*User, error) {
	expr, err := expression.NewBuilder().WithKeyCondition(key).Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	ctx, cancel := opContext()
	defer cancel()
	out, err := repo.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &repo.table,
		IndexName:                 aws.String(index),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		Limit:                     aws.Int32(1),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query %s: %w", index, err)
	}
	if len(out.Items) == 0 {
		return nil, ErrNotFound
	}
	return unmarshalUser(out.Items[0])
}

func unmarshalUser(item map[string]types.AttributeValue) (*User, error) {
	var u User
	if err := attributevalue.UnmarshalMap(item, &u); err != nil {
		return nil, fmt.Errorf("failed to decode user: %w", err)
	}
	return &u, nil
}

// ---------------------------------------------------------------------------
// Updates. Each one writes a fixed set of attributes, then mirrors the change
// in the in-memory user so handlers keep working with current data.
// ---------------------------------------------------------------------------

// ResetState returns the user to Idle and clears any conversation in progress.
func (repo *UserRepository) ResetState(u *User) error {
	if err := repo.update(u, idle()); err != nil {
		return fmt.Errorf("failed to reset user state: %w", err)
	}
	u.State, u.ContactUUID, u.ReplyMessageID = Idle, "", 0
	return nil
}

// StartSending makes the user's next message go to contactUUID, as a reply to
// replyToMessageID when it isn't 0.
func (repo *UserRepository) StartSending(u *User, contactUUID string, replyToMessageID int64) error {
	upd := expression.Set(expression.Name("State"), expression.Value(Sending)).
		Set(expression.Name("ContactUUID"), expression.Value(contactUUID))
	if replyToMessageID != 0 {
		upd = upd.Set(expression.Name("ReplyMessageID"), expression.Value(replyToMessageID))
	} else {
		upd = upd.Remove(expression.Name("ReplyMessageID"))
	}
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to start sending: %w", err)
	}
	u.State, u.ContactUUID, u.ReplyMessageID = Sending, contactUUID, replyToMessageID
	return nil
}

// StartSettingUsername makes the user's next message their new username.
func (repo *UserRepository) StartSettingUsername(u *User) error {
	upd := expression.Set(expression.Name("State"), expression.Value(SettingUsername)).
		Remove(expression.Name("ContactUUID")).
		Remove(expression.Name("ReplyMessageID"))
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to start setting username: %w", err)
	}
	u.State, u.ContactUUID, u.ReplyMessageID = SettingUsername, "", 0
	return nil
}

// SetUsername stores a username and returns the user to Idle.
func (repo *UserRepository) SetUsername(u *User, username string) error {
	if username == "" {
		return errors.New("username must not be empty; use RemoveUsername")
	}
	if err := repo.update(u, idle().Set(expression.Name("Username"), expression.Value(username))); err != nil {
		return fmt.Errorf("failed to set username: %w", err)
	}
	u.Username = username
	u.State, u.ContactUUID, u.ReplyMessageID = Idle, "", 0
	return nil
}

// RemoveUsername deletes the user's username and returns them to Idle. The
// attribute is removed rather than emptied, since it's an index key.
func (repo *UserRepository) RemoveUsername(u *User) error {
	if err := repo.update(u, idle().Remove(expression.Name("Username"))); err != nil {
		return fmt.Errorf("failed to remove username: %w", err)
	}
	u.Username = ""
	u.State, u.ContactUUID, u.ReplyMessageID = Idle, "", 0
	return nil
}

// SetLanguage stores the user's preferred language and returns them to Idle.
func (repo *UserRepository) SetLanguage(u *User, language i18n.Language) error {
	if err := repo.update(u, idle().Set(expression.Name("Language"), expression.Value(language))); err != nil {
		return fmt.Errorf("failed to set language: %w", err)
	}
	u.Language = language
	u.State, u.ContactUUID, u.ReplyMessageID = Idle, "", 0
	return nil
}

// Block adds another user's UUID to this user's blacklist.
func (repo *UserRepository) Block(u *User, blockedUUID string) error {
	upd := expression.Add(expression.Name("Blacklist"), expression.Value(stringSet{blockedUUID}))
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to block user: %w", err)
	}
	if !slices.Contains(u.Blacklist, blockedUUID) {
		u.Blacklist = append(u.Blacklist, blockedUUID)
	}
	return nil
}

// Unblock removes another user's UUID from this user's blacklist.
func (repo *UserRepository) Unblock(u *User, blockedUUID string) error {
	upd := expression.Delete(expression.Name("Blacklist"), expression.Value(stringSet{blockedUUID}))
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to unblock user: %w", err)
	}
	u.Blacklist = slices.DeleteFunc(slices.Clone(u.Blacklist), func(s string) bool { return s == blockedUUID })
	return nil
}

// UnblockAll clears this user's blacklist.
func (repo *UserRepository) UnblockAll(u *User) error {
	if err := repo.update(u, expression.Remove(expression.Name("Blacklist"))); err != nil {
		return fmt.Errorf("failed to unblock all users: %w", err)
	}
	u.Blacklist = nil
	return nil
}

// idle is the update that returns a user to Idle with no conversation.
func idle() expression.UpdateBuilder {
	return expression.Set(expression.Name("State"), expression.Value(Idle)).
		Remove(expression.Name("ContactUUID")).
		Remove(expression.Name("ReplyMessageID"))
}

// update applies upd to the user's item. The item must already exist, so an
// update never creates a partial user.
func (repo *UserRepository) update(u *User, upd expression.UpdateBuilder) error {
	expr, err := expression.NewBuilder().
		WithUpdate(upd).
		WithCondition(expression.AttributeExists(expression.Name("UUID"))).
		Build()
	if err != nil {
		return fmt.Errorf("failed to build update: %w", err)
	}

	ctx, cancel := opContext()
	defer cancel()
	_, err = repo.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &repo.table,
		Key:                       itemKey(u.UUID),
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	return err
}

// stringSet marshals as a DynamoDB string set (SS), which ADD and DELETE
// require; a plain []string would be encoded as a list.
type stringSet []string

func (s stringSet) MarshalDynamoDBAttributeValue() (types.AttributeValue, error) {
	return &types.AttributeValueMemberSS{Value: s}, nil
}
