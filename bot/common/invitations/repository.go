package invitations

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bugfloyd/telegram-incognito-bot/common/db"
	"github.com/bugfloyd/telegram-incognito-bot/common/users"
)

// TableName must match the invitations table in infra/main.tf.
const TableName = "IncognitoChatBot-Invitations"

// ownerIndex lists a user's codes, newest first (infra/main.tf).
const ownerIndex = "OwnerUUID-GSI"

// Item IDs in the invitations table.
const (
	codeIDPrefix  = "code#"
	allowancesID  = "settings#level-allowances"
	maxCodeTries  = 5
	maxSettingTry = 3
)

var (
	ErrCodeNotFound      = errors.New("invitation code not found")
	ErrCodeUsedUp        = errors.New("invitation code used up")
	ErrAlreadyRegistered = errors.New("user is already registered")
	ErrNotRegistered     = errors.New("user is not registered")
	ErrNoInvitationsLeft = errors.New("not enough invitations left")
)

// Code is an invitation code that can register up to UsesTotal people.
type Code struct {
	ID         string    `dynamodbav:"ID"`
	OwnerUUID  string    `dynamodbav:"OwnerUUID"`
	OwnerLevel int       `dynamodbav:"OwnerLevel"` // 0 for admins
	UsesTotal  int       `dynamodbav:"UsesTotal"`
	UsesLeft   int       `dynamodbav:"UsesLeft"`
	CreatedAt  time.Time `dynamodbav:"CreatedAt,unixtime"`
}

// Code returns the code people type, e.g. "abcd-efgh".
func (c Code) Code() string {
	return c.ID[len(codeIDPrefix):]
}

type levelAllowances struct {
	ID         string         `dynamodbav:"ID"`
	Allowances map[string]int `dynamodbav:"Allowances"`
	Version    int            `dynamodbav:"Version"`
}

type Repository struct {
	client     *dynamodb.Client
	table      string
	usersTable string
}

func NewRepository() (*Repository, error) {
	return newRepository(TableName, users.TableName)
}

func newRepository(table, usersTable string) (*Repository, error) {
	client, err := db.Client()
	if err != nil {
		return nil, err
	}
	return &Repository{client: client, table: table, usersTable: usersTable}, nil
}

func idKey(id string) map[string]types.AttributeValue {
	return map[string]types.AttributeValue{"ID": &types.AttributeValueMemberS{Value: id}}
}

// ---------------------------------------------------------------------------
// Level allowances
// ---------------------------------------------------------------------------

// LevelAllowances returns the invitation allowance of every level that has
// one; levels not listed have 0.
func (repo *Repository) LevelAllowances() (map[int]int, error) {
	settings, err := repo.readAllowances()
	if err != nil {
		return nil, err
	}
	out := make(map[int]int, len(settings.Allowances))
	for level, n := range settings.Allowances {
		if l, err := strconv.Atoi(level); err == nil {
			out[l] = n
		}
	}
	return out, nil
}

// LevelAllowance returns one level's invitation allowance.
func (repo *Repository) LevelAllowance(level int) (int, error) {
	all, err := repo.LevelAllowances()
	if err != nil {
		return 0, err
	}
	return all[level], nil
}

// SetLevelAllowance sets how many invitations every user at a level gets. It
// applies immediately to existing users at that level.
func (repo *Repository) SetLevelAllowance(level, invitations int) error {
	if level < 1 || invitations < 0 {
		return fmt.Errorf("invalid level %d or allowance %d", level, invitations)
	}
	for try := 0; try < maxSettingTry; try++ {
		settings, err := repo.readAllowances()
		if err != nil {
			return err
		}
		previous := settings.Version
		settings.ID = allowancesID
		settings.Allowances[strconv.Itoa(level)] = invitations
		settings.Version++

		item, err := attributevalue.MarshalMap(settings)
		if err != nil {
			return fmt.Errorf("failed to encode allowances: %w", err)
		}
		// Optimistic locking: only write if nobody changed the settings meanwhile.
		cond := expression.AttributeNotExists(expression.Name("ID")).
			Or(expression.Name("Version").Equal(expression.Value(previous)))
		expr, err := expression.NewBuilder().WithCondition(cond).Build()
		if err != nil {
			return err
		}
		ctx, cancel := db.OpContext()
		_, err = repo.client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName:                 &repo.table,
			Item:                      item,
			ConditionExpression:       expr.Condition(),
			ExpressionAttributeNames:  expr.Names(),
			ExpressionAttributeValues: expr.Values(),
		})
		cancel()
		if db.IsConditionFailed(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("failed to save allowances: %w", err)
		}
		return nil
	}
	return errors.New("allowances changed concurrently; try again")
}

func (repo *Repository) readAllowances() (*levelAllowances, error) {
	ctx, cancel := db.OpContext()
	defer cancel()
	out, err := repo.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      &repo.table,
		Key:            idKey(allowancesID),
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read allowances: %w", err)
	}
	settings := &levelAllowances{Allowances: map[string]int{}}
	if out.Item != nil {
		if err := attributevalue.UnmarshalMap(out.Item, settings); err != nil {
			return nil, fmt.Errorf("failed to decode allowances: %w", err)
		}
		if settings.Allowances == nil {
			settings.Allowances = map[string]int{}
		}
	}
	return settings, nil
}

// Remaining returns how many invitation uses a registered user can still give out.
func (repo *Repository) Remaining(u *users.User) (int, error) {
	if !u.Registered() {
		return 0, nil
	}
	allowance, err := repo.LevelAllowance(u.Level)
	if err != nil {
		return 0, err
	}
	return Remaining(allowance, u.InvitesGranted, u.InvitesAllocated), nil
}

// Grant gives a registered user extra invitations on top of their level's allowance.
func (repo *Repository) Grant(u *users.User, invitations int) error {
	if invitations < 1 {
		return fmt.Errorf("invalid number of invitations: %d", invitations)
	}
	cond := expression.AttributeExists(expression.Name("UUID")).
		And(expression.AttributeExists(expression.Name("Level")))
	upd := expression.Add(expression.Name("InvitesGranted"), expression.Value(invitations))
	expr, err := expression.NewBuilder().WithUpdate(upd).WithCondition(cond).Build()
	if err != nil {
		return err
	}
	ctx, cancel := db.OpContext()
	defer cancel()
	_, err = repo.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName:                 &repo.usersTable,
		Key:                       users.Key(u.UUID),
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if db.IsConditionFailed(err) {
		return ErrNotRegistered
	}
	if err != nil {
		return fmt.Errorf("failed to grant invitations: %w", err)
	}
	u.InvitesGranted += invitations
	return nil
}

// ---------------------------------------------------------------------------
// Codes
// ---------------------------------------------------------------------------

// CreateCode creates a code that can register `uses` people. Admins
// (unlimited = true) create level-1 invitations without a budget; other owners
// must be registered and spend `uses` from their remaining invitations, which
// is enforced atomically so concurrent requests can't overspend.
func (repo *Repository) CreateCode(owner *users.User, uses int, unlimited bool) (*Code, error) {
	if uses < 1 {
		return nil, fmt.Errorf("invalid number of uses: %d", uses)
	}
	ownerLevel := 0
	limit := 0
	if !unlimited {
		if !owner.Registered() {
			return nil, ErrNotRegistered
		}
		ownerLevel = owner.Level
		allowance, err := repo.LevelAllowance(owner.Level)
		if err != nil {
			return nil, err
		}
		limit = allowance + owner.InvitesGranted
		if uses > limit {
			return nil, ErrNoInvitationsLeft
		}
	}

	for try := 0; try < maxCodeTries; try++ {
		value, err := NewCode()
		if err != nil {
			return nil, fmt.Errorf("failed to generate code: %w", err)
		}
		code := &Code{
			ID:         codeIDPrefix + value,
			OwnerUUID:  owner.UUID,
			OwnerLevel: ownerLevel,
			UsesTotal:  uses,
			UsesLeft:   uses,
			CreatedAt:  time.Unix(time.Now().Unix(), 0),
		}
		putCode, err := repo.putNewCode(code)
		if err != nil {
			return nil, err
		}
		items := []types.TransactWriteItem{putCode}
		if !unlimited {
			spend, err := repo.spendInvitations(owner, uses, limit)
			if err != nil {
				return nil, err
			}
			items = append(items, spend)
		}

		ctx, cancel := db.OpContext()
		_, err = repo.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{TransactItems: items})
		cancel()
		if reasons := db.CancellationReasons(err); reasons != nil {
			if len(reasons) > 1 && reasons[1] == db.ConditionFailed {
				return nil, ErrNoInvitationsLeft
			}
			if reasons[0] == db.ConditionFailed || slices.Contains(reasons, db.TransactionConflict) {
				// Code collision, or a concurrent change to the owner: try again.
				db.RetryPause(try)
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create invitation code: %w", err)
		}
		if !unlimited {
			owner.InvitesAllocated += uses
		}
		return code, nil
	}
	return nil, errors.New("failed to find an unused invitation code")
}

func (repo *Repository) putNewCode(code *Code) (types.TransactWriteItem, error) {
	item, err := attributevalue.MarshalMap(code)
	if err != nil {
		return types.TransactWriteItem{}, fmt.Errorf("failed to encode code: %w", err)
	}
	return types.TransactWriteItem{Put: &types.Put{
		TableName:                &repo.table,
		Item:                     item,
		ConditionExpression:      aws.String("attribute_not_exists(#id)"),
		ExpressionAttributeNames: map[string]string{"#id": "ID"},
	}}, nil
}

// spendInvitations adds `uses` to the owner's allocated invitations, only if
// the total stays within limit (and the owner is still registered).
func (repo *Repository) spendInvitations(owner *users.User, uses, limit int) (types.TransactWriteItem, error) {
	allocated := expression.Name("InvitesAllocated")
	cond := expression.AttributeExists(expression.Name("UUID")).
		And(expression.AttributeExists(expression.Name("Level"))).
		And(expression.AttributeNotExists(allocated).Or(allocated.LessThanEqual(expression.Value(limit - uses))))
	expr, err := expression.NewBuilder().
		WithUpdate(expression.Add(allocated, expression.Value(uses))).
		WithCondition(cond).
		Build()
	if err != nil {
		return types.TransactWriteItem{}, err
	}
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 &repo.usersTable,
		Key:                       users.Key(owner.UUID),
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}}, nil
}

// RedeemCode registers invitee with a code: one use is taken from the code and
// the invitee joins at the code owner's level + 1, all or nothing. It returns
// the invitee's new level. Who invited whom is not recorded.
func (repo *Repository) RedeemCode(value string, invitee *users.User) (int, error) {
	if invitee.Registered() {
		return 0, ErrAlreadyRegistered
	}
	code, err := repo.readCode(value)
	if err != nil {
		return 0, err
	}
	if code.UsesLeft < 1 {
		return 0, ErrCodeUsedUp
	}
	level := code.OwnerLevel + 1

	useCode, err := conditionalUpdate(repo.table, idKey(code.ID),
		expression.Set(expression.Name("UsesLeft"), expression.Name("UsesLeft").Minus(expression.Value(1))),
		expression.AttributeExists(expression.Name("ID")).
			And(expression.Name("UsesLeft").GreaterThanEqual(expression.Value(1))))
	if err != nil {
		return 0, err
	}
	register, err := conditionalUpdate(repo.usersTable, users.Key(invitee.UUID),
		expression.Set(expression.Name("Level"), expression.Value(level)),
		expression.AttributeExists(expression.Name("UUID")).
			And(expression.AttributeNotExists(expression.Name("Level"))))
	if err != nil {
		return 0, err
	}

	for try := 0; ; try++ {
		ctx, cancel := db.OpContext()
		_, err = repo.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
			TransactItems: []types.TransactWriteItem{useCode, register},
		})
		cancel()
		if reasons := db.CancellationReasons(err); reasons != nil {
			switch {
			case reasons[1] == db.ConditionFailed:
				return 0, ErrAlreadyRegistered
			case reasons[0] == db.ConditionFailed:
				return 0, ErrCodeUsedUp
			case slices.Contains(reasons, db.TransactionConflict) && try < maxCodeTries:
				// Someone redeemed the same code at the same moment: try again.
				db.RetryPause(try)
				continue
			}
		}
		if err != nil {
			return 0, fmt.Errorf("failed to redeem invitation code: %w", err)
		}
		invitee.Level = level
		return level, nil
	}
}

func conditionalUpdate(table string, key map[string]types.AttributeValue, upd expression.UpdateBuilder, cond expression.ConditionBuilder) (types.TransactWriteItem, error) {
	expr, err := expression.NewBuilder().WithUpdate(upd).WithCondition(cond).Build()
	if err != nil {
		return types.TransactWriteItem{}, err
	}
	return types.TransactWriteItem{Update: &types.Update{
		TableName:                 aws.String(table),
		Key:                       key,
		UpdateExpression:          expr.Update(),
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	}}, nil
}

func (repo *Repository) readCode(value string) (*Code, error) {
	ctx, cancel := db.OpContext()
	defer cancel()
	out, err := repo.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &repo.table,
		Key:       idKey(codeIDPrefix + value),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read invitation code: %w", err)
	}
	if out.Item == nil {
		return nil, ErrCodeNotFound
	}
	var code Code
	if err := attributevalue.UnmarshalMap(out.Item, &code); err != nil {
		return nil, fmt.Errorf("failed to decode invitation code: %w", err)
	}
	return &code, nil
}

// ListCodes returns a user's most recent invitation codes, newest first.
func (repo *Repository) ListCodes(ownerUUID string, limit int32) ([]Code, error) {
	expr, err := expression.NewBuilder().
		WithKeyCondition(expression.Key("OwnerUUID").Equal(expression.Value(ownerUUID))).
		Build()
	if err != nil {
		return nil, err
	}
	ctx, cancel := db.OpContext()
	defer cancel()
	out, err := repo.client.Query(ctx, &dynamodb.QueryInput{
		TableName:                 &repo.table,
		IndexName:                 aws.String(ownerIndex),
		KeyConditionExpression:    expr.KeyCondition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
		ScanIndexForward:          aws.Bool(false),
		Limit:                     aws.Int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list invitation codes: %w", err)
	}
	var codes []Code
	if err := attributevalue.UnmarshalListOfMaps(out.Items, &codes); err != nil {
		return nil, fmt.Errorf("failed to decode invitation codes: %w", err)
	}
	return codes, nil
}
