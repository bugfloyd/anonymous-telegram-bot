package users

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bugfloyd/telegram-incognito-bot/common/db"
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

// ErrNotFound is returned when no user matches a lookup.
var ErrNotFound = errors.New("user not found")

type UserRepository struct {
	client *dynamodb.Client
	table  string
}

func NewUserRepository() (*UserRepository, error) {
	return newUserRepository(TableName)
}

func newUserRepository(table string) (*UserRepository, error) {
	client, err := db.Client()
	if err != nil {
		return nil, err
	}
	return &UserRepository{client: client, table: table}, nil
}

// Key is the primary key of a user's item (used by other packages' transactions).
func Key(userUUID string) map[string]types.AttributeValue {
	return itemKey(userUUID)
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

	ctx, cancel := db.OpContext()
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
	ctx, cancel := db.OpContext()
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

	ctx, cancel := db.OpContext()
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
	u.setIdle()
	return nil
}

// StartSending makes the user's next message go to contactUUID, as a reply to
// replyToMessageID when it isn't 0.
func (repo *UserRepository) StartSending(u *User, contactUUID string, replyToMessageID int64) error {
	upd := expression.Set(expression.Name("State"), expression.Value(Sending)).
		Set(expression.Name("ContactUUID"), expression.Value(contactUUID)).
		Remove(expression.Name("LastSentAt"))
	if replyToMessageID != 0 {
		upd = upd.Set(expression.Name("ReplyMessageID"), expression.Value(replyToMessageID))
	} else {
		upd = upd.Remove(expression.Name("ReplyMessageID"))
	}
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to start sending: %w", err)
	}
	u.State, u.ContactUUID, u.ReplyMessageID, u.LastSentAt = Sending, contactUUID, replyToMessageID, 0
	return nil
}

// StartSettingUsername makes the user's next message their new username.
func (repo *UserRepository) StartSettingUsername(u *User) error {
	upd := expression.Set(expression.Name("State"), expression.Value(SettingUsername)).
		Remove(expression.Name("ContactUUID")).
		Remove(expression.Name("ReplyMessageID")).
		Remove(expression.Name("LastSentAt"))
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to start setting username: %w", err)
	}
	u.State, u.ContactUUID, u.ReplyMessageID, u.LastSentAt = SettingUsername, "", 0, 0
	return nil
}

// StartEnteringInvitationCode makes the user's next message an invitation code.
func (repo *UserRepository) StartEnteringInvitationCode(u *User) error {
	return repo.startPrompt(u, EnteringInvitationCode)
}

// StartCreatingInvitation makes the user's next message the number of uses for
// a new invitation code.
func (repo *UserRepository) StartCreatingInvitation(u *User) error {
	return repo.startPrompt(u, CreatingInvitation)
}

// startPrompt puts the user in a state where their next message answers a
// question from the bot, with no conversation open.
func (repo *UserRepository) startPrompt(u *User, state State) error {
	upd := expression.Set(expression.Name("State"), expression.Value(state)).
		Remove(expression.Name("ContactUUID")).
		Remove(expression.Name("ReplyMessageID")).
		Remove(expression.Name("LastSentAt"))
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to set state %s: %w", state, err)
	}
	u.State, u.ContactUUID, u.ReplyMessageID, u.LastSentAt = state, "", 0, 0
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
	u.setIdle()
	return nil
}

// RemoveUsername deletes the user's username and returns them to Idle. The
// attribute is removed rather than emptied, since it's an index key.
func (repo *UserRepository) RemoveUsername(u *User) error {
	if err := repo.update(u, idle().Remove(expression.Name("Username"))); err != nil {
		return fmt.Errorf("failed to remove username: %w", err)
	}
	u.Username = ""
	u.setIdle()
	return nil
}

// SetLanguage stores the user's preferred language and returns them to Idle.
func (repo *UserRepository) SetLanguage(u *User, language i18n.Language) error {
	if err := repo.update(u, idle().Set(expression.Name("Language"), expression.Value(language))); err != nil {
		return fmt.Errorf("failed to set language: %w", err)
	}
	u.Language = language
	u.setIdle()
	return nil
}

// ContinueConversation keeps the conversation open after a message was
// delivered at sentAt: the user's follow-ups go to the same person, threaded
// the same way, until InterruptFollowUps or the follow-up window ends it.
func (repo *UserRepository) ContinueConversation(u *User, sentAt time.Time) error {
	upd := expression.Set(expression.Name("State"), expression.Value(Continuing)).
		Set(expression.Name("LastSentAt"), expression.Value(sentAt.Unix()))
	if err := repo.update(u, upd); err != nil {
		return fmt.Errorf("failed to continue conversation: %w", err)
	}
	u.State, u.LastSentAt = Continuing, sentAt.Unix()
	return nil
}

// InterruptFollowUps ends a user's follow-ups because the bot is showing them
// something from fromUUID. It only acts when the user is Continuing a
// conversation with someone else: a message from the same person doesn't
// interrupt, and explicitly chosen states (e.g. after pressing Reply) are left
// alone. The check and the write are one conditional update, so concurrent
// changes can't slip in between. Reports whether follow-ups were ended.
func (repo *UserRepository) InterruptFollowUps(userUUID, fromUUID string) (bool, error) {
	cond := expression.AttributeExists(expression.Name("UUID")).
		And(expression.Name("State").Equal(expression.Value(Continuing))).
		And(expression.Name("ContactUUID").NotEqual(expression.Value(fromUUID)))
	err := repo.updateWhere(userUUID, idle(), cond)
	if db.IsConditionFailed(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to interrupt follow-ups: %w", err)
	}
	return true, nil
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
		Remove(expression.Name("ReplyMessageID")).
		Remove(expression.Name("LastSentAt"))
}

// setIdle mirrors idle() on the in-memory user.
func (u *User) setIdle() {
	u.State, u.ContactUUID, u.ReplyMessageID, u.LastSentAt = Idle, "", 0, 0
}

// update applies upd to the user's item. The item must already exist, so an
// update never creates a partial user.
func (repo *UserRepository) update(u *User, upd expression.UpdateBuilder) error {
	return repo.updateWhere(u.UUID, upd, expression.AttributeExists(expression.Name("UUID")))
}

// updateWhere applies upd to a user's item only if cond holds.
func (repo *UserRepository) updateWhere(userUUID string, upd expression.UpdateBuilder, cond expression.ConditionBuilder) error {
	expr, err := expression.NewBuilder().WithUpdate(upd).WithCondition(cond).Build()
	if err != nil {
		return fmt.Errorf("failed to build update: %w", err)
	}

	ctx, cancel := db.OpContext()
	defer cancel()
	_, err = repo.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &repo.table,
		Key:                       itemKey(userUUID),
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
