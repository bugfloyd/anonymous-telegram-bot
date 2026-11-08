package invitations

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bugfloyd/telegram-incognito-bot/common/users"
	"github.com/google/uuid"
)

// These tests run against DynamoDB Local when DYNAMODB_ENDPOINT is set (CI
// starts it as a service container) and are skipped otherwise; see the README.

type testEnv struct {
	repo *Repository
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	if os.Getenv("DYNAMODB_ENDPOINT") == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("DYNAMODB_ENDPOINT must be set in CI so the DynamoDB integration tests run")
		}
		t.Skip("DYNAMODB_ENDPOINT not set; skipping DynamoDB integration tests")
	}
	suffix := time.Now().UnixNano()
	repo, err := newRepository(fmt.Sprintf("%s-test-%d", TableName, suffix), fmt.Sprintf("%s-test-%d", users.TableName, suffix))
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	attr := func(name string, typ types.ScalarAttributeType) types.AttributeDefinition {
		return types.AttributeDefinition{AttributeName: aws.String(name), AttributeType: typ}
	}
	key := func(name string, typ types.KeyType) types.KeySchemaElement {
		return types.KeySchemaElement{AttributeName: aws.String(name), KeyType: typ}
	}
	// Same shapes as infra/main.tf (the users table's GSIs aren't needed here).
	createTable(t, repo.client, &dynamodb.CreateTableInput{
		TableName:            &repo.usersTable,
		BillingMode:          types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{attr("UUID", types.ScalarAttributeTypeS)},
		KeySchema:            []types.KeySchemaElement{key("UUID", types.KeyTypeHash)},
	})
	createTable(t, repo.client, &dynamodb.CreateTableInput{
		TableName:   &repo.table,
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			attr("ID", types.ScalarAttributeTypeS),
			attr("OwnerUUID", types.ScalarAttributeTypeS),
			attr("CreatedAt", types.ScalarAttributeTypeN),
		},
		KeySchema: []types.KeySchemaElement{key("ID", types.KeyTypeHash)},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{{
			IndexName:  aws.String(ownerIndex),
			KeySchema:  []types.KeySchemaElement{key("OwnerUUID", types.KeyTypeHash), key("CreatedAt", types.KeyTypeRange)},
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		}},
	})
	return &testEnv{repo: repo}
}

func createTable(t *testing.T, client *dynamodb.Client, input *dynamodb.CreateTableInput) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := client.CreateTable(ctx, input); err != nil {
		t.Fatalf("create table %s: %v", *input.TableName, err)
	}
	waiter := dynamodb.NewTableExistsWaiter(client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: input.TableName}, time.Minute); err != nil {
		t.Fatalf("wait for table %s: %v", *input.TableName, err)
	}
	t.Cleanup(func() {
		if _, err := client.DeleteTable(context.Background(), &dynamodb.DeleteTableInput{TableName: input.TableName}); err != nil {
			t.Errorf("delete table %s: %v", *input.TableName, err)
		}
	})
}

// newUser stores a user at the given level (0 = not registered).
func (env *testEnv) newUser(t *testing.T, level int) *users.User {
	t.Helper()
	u := &users.User{UUID: uuid.NewString(), UserID: time.Now().UnixNano(), State: users.Idle, Level: level, CreatedAt: time.Unix(time.Now().Unix(), 0)}
	item, err := attributevalue.MarshalMap(u)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.repo.client.PutItem(context.Background(), &dynamodb.PutItemInput{TableName: &env.repo.usersTable, Item: item}); err != nil {
		t.Fatalf("put user: %v", err)
	}
	return u
}

// stored reads a user back from the table.
func (env *testEnv) stored(t *testing.T, u *users.User) *users.User {
	t.Helper()
	out, err := env.repo.client.GetItem(context.Background(), &dynamodb.GetItemInput{TableName: &env.repo.usersTable, Key: users.Key(u.UUID), ConsistentRead: aws.Bool(true)})
	if err != nil || out.Item == nil {
		t.Fatalf("get user: %v", err)
	}
	var got users.User
	if err := attributevalue.UnmarshalMap(out.Item, &got); err != nil {
		t.Fatal(err)
	}
	return &got
}

func TestLevelAllowances(t *testing.T) {
	env := newTestEnv(t)
	if n, err := env.repo.LevelAllowance(1); err != nil || n != 0 {
		t.Fatalf("unset allowance = %d, %v; want 0", n, err)
	}
	for _, step := range []struct{ level, n int }{{1, 5}, {2, 2}, {1, 7}} {
		if err := env.repo.SetLevelAllowance(step.level, step.n); err != nil {
			t.Fatalf("SetLevelAllowance(%d, %d): %v", step.level, step.n, err)
		}
	}
	all, err := env.repo.LevelAllowances()
	if err != nil || all[1] != 7 || all[2] != 2 || all[3] != 0 {
		t.Errorf("LevelAllowances = %v, %v; want level 1 = 7, level 2 = 2", all, err)
	}
	if err := env.repo.SetLevelAllowance(0, 3); err == nil {
		t.Error("level 0 (admins) has no allowance to set")
	}
}

func TestAdminInvitationRegistersLevelOne(t *testing.T) {
	env := newTestEnv(t)
	admin := env.newUser(t, 0)
	code, err := env.repo.CreateCode(admin, 2, true)
	if err != nil {
		t.Fatalf("CreateCode: %v", err)
	}
	if code.OwnerLevel != 0 || code.UsesTotal != 2 || code.UsesLeft != 2 || code.OwnerUUID != admin.UUID {
		t.Errorf("unexpected code: %+v", code)
	}
	if env.stored(t, admin).InvitesAllocated != 0 {
		t.Error("admin invitations must not be counted against a budget")
	}

	alice := env.newUser(t, 0)
	if level, err := env.repo.RedeemCode(code.Code(), alice); err != nil || level != 1 {
		t.Fatalf("RedeemCode = %d, %v; want level 1", level, err)
	}
	if alice.Level != 1 || env.stored(t, alice).Level != 1 {
		t.Error("alice should be registered at level 1")
	}

	// Redeeming again can't register twice, and doesn't use up the code.
	if _, err := env.repo.RedeemCode(code.Code(), alice); !errors.Is(err, ErrAlreadyRegistered) {
		t.Errorf("second redeem: %v, want ErrAlreadyRegistered", err)
	}
	stale := *alice
	stale.Level = 0 // e.g. a handler holding an outdated copy
	if _, err := env.repo.RedeemCode(code.Code(), &stale); !errors.Is(err, ErrAlreadyRegistered) {
		t.Errorf("stale redeem: %v, want ErrAlreadyRegistered", err)
	}
	if got, _ := env.repo.readCode(code.Code()); got.UsesLeft != 1 {
		t.Errorf("UsesLeft = %d after failed redemptions, want 1 (all or nothing)", got.UsesLeft)
	}

	bob := env.newUser(t, 0)
	if _, err := env.repo.RedeemCode(code.Code(), bob); err != nil {
		t.Fatalf("bob: %v", err)
	}
	carol := env.newUser(t, 0)
	if _, err := env.repo.RedeemCode(code.Code(), carol); !errors.Is(err, ErrCodeUsedUp) {
		t.Errorf("carol: %v, want ErrCodeUsedUp", err)
	}
	if env.stored(t, carol).Level != 0 {
		t.Error("carol must not be registered")
	}
	if _, err := env.repo.RedeemCode("zzzz-zzzz", carol); !errors.Is(err, ErrCodeNotFound) {
		t.Errorf("unknown code: %v, want ErrCodeNotFound", err)
	}
}

func TestBudgetsAndLevels(t *testing.T) {
	env := newTestEnv(t)
	if err := env.repo.SetLevelAllowance(1, 3); err != nil {
		t.Fatal(err)
	}
	owner := env.newUser(t, 1)

	if _, err := env.repo.CreateCode(env.newUser(t, 0), 1, false); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("unregistered owner: %v, want ErrNotRegistered", err)
	}

	code, err := env.repo.CreateCode(owner, 2, false)
	if err != nil {
		t.Fatalf("CreateCode(2): %v", err)
	}
	if _, err := env.repo.CreateCode(owner, 2, false); !errors.Is(err, ErrNoInvitationsLeft) {
		t.Errorf("over budget: %v, want ErrNoInvitationsLeft", err)
	}
	if left, _ := env.repo.Remaining(env.stored(t, owner)); left != 1 {
		t.Errorf("remaining = %d, want 1", left)
	}

	// Raising the level allowance applies to existing users immediately.
	if err := env.repo.SetLevelAllowance(1, 5); err != nil {
		t.Fatal(err)
	}
	if left, _ := env.repo.Remaining(env.stored(t, owner)); left != 3 {
		t.Errorf("remaining after raise = %d, want 3", left)
	}
	// A personal grant adds on top.
	if err := env.repo.Grant(owner, 2); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if left, _ := env.repo.Remaining(env.stored(t, owner)); left != 5 {
		t.Errorf("remaining after grant = %d, want 5", left)
	}
	if err := env.repo.Grant(env.newUser(t, 0), 2); !errors.Is(err, ErrNotRegistered) {
		t.Errorf("grant to unregistered: %v, want ErrNotRegistered", err)
	}

	// Someone invited by a level-1 user joins at level 2.
	invitee := env.newUser(t, 0)
	if level, err := env.repo.RedeemCode(code.Code(), invitee); err != nil || level != 2 {
		t.Errorf("RedeemCode = %d, %v; want level 2", level, err)
	}

	codes, err := env.repo.ListCodes(owner.UUID, 10)
	if err != nil || len(codes) != 1 || codes[0].Code() != code.Code() || codes[0].UsesLeft != 1 {
		t.Errorf("ListCodes = %+v, %v", codes, err)
	}
}

func TestConcurrentCodeCreationCannotOverspend(t *testing.T) {
	env := newTestEnv(t)
	if err := env.repo.SetLevelAllowance(1, 3); err != nil {
		t.Fatal(err)
	}
	owner := env.newUser(t, 1)

	var created atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			copy := *owner // each request works on its own copy, like separate Lambda invocations
			if _, err := env.repo.CreateCode(&copy, 1, false); err == nil {
				created.Add(1)
			} else if !errors.Is(err, ErrNoInvitationsLeft) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if created.Load() != 3 {
		t.Errorf("created %d codes with a budget of 3", created.Load())
	}
	if got := env.stored(t, owner).InvitesAllocated; got != 3 {
		t.Errorf("InvitesAllocated = %d, want 3", got)
	}
}

func TestConcurrentRedemptionCannotExceedUses(t *testing.T) {
	env := newTestEnv(t)
	code, err := env.repo.CreateCode(env.newUser(t, 0), 2, true)
	if err != nil {
		t.Fatal(err)
	}
	var registered atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		invitee := env.newUser(t, 0)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := env.repo.RedeemCode(code.Code(), invitee); err == nil {
				registered.Add(1)
			} else if !errors.Is(err, ErrCodeUsedUp) {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()
	if registered.Load() != 2 {
		t.Errorf("%d people registered with a 2-use code", registered.Load())
	}
	if got, _ := env.repo.readCode(code.Code()); got.UsesLeft != 0 {
		t.Errorf("UsesLeft = %d, want 0", got.UsesLeft)
	}
}
