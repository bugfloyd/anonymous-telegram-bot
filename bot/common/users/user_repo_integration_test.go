package users

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/bugfloyd/telegram-incognito-bot/common/conversation"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
)

// These tests run against DynamoDB Local when DYNAMODB_ENDPOINT is set (CI
// starts it as a service container) and are skipped otherwise. To run them
// locally:
//
//	docker run --rm -p 8000:8000 amazon/dynamodb-local:3.3.2
//	DYNAMODB_ENDPOINT=http://localhost:8000 AWS_REGION=local \
//	AWS_ACCESS_KEY_ID=dummy AWS_SECRET_ACCESS_KEY=dummy go test ./common/users/
func newTestRepo(t *testing.T) *UserRepository {
	t.Helper()
	if os.Getenv("DYNAMODB_ENDPOINT") == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("DYNAMODB_ENDPOINT must be set in CI so the DynamoDB integration tests run")
		}
		t.Skip("DYNAMODB_ENDPOINT not set; skipping DynamoDB integration tests")
	}
	repo, err := newUserRepository(fmt.Sprintf("%s-test-%d", TableName, time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	createTable(t, repo)
	return repo
}

// createTable creates a table shaped like the one in infra/main.tf:
// on-demand, keyed by UUID, with three GSIs projecting all attributes.
func createTable(t *testing.T, repo *UserRepository) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	attr := func(name string, typ types.ScalarAttributeType) types.AttributeDefinition {
		return types.AttributeDefinition{AttributeName: aws.String(name), AttributeType: typ}
	}
	key := func(name string, typ types.KeyType) types.KeySchemaElement {
		return types.KeySchemaElement{AttributeName: aws.String(name), KeyType: typ}
	}
	gsi := func(name string, keys ...types.KeySchemaElement) types.GlobalSecondaryIndex {
		return types.GlobalSecondaryIndex{
			IndexName:  aws.String(name),
			KeySchema:  keys,
			Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
		}
	}
	_, err := repo.client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName:   &repo.table,
		BillingMode: types.BillingModePayPerRequest,
		AttributeDefinitions: []types.AttributeDefinition{
			attr("UUID", types.ScalarAttributeTypeS),
			attr("UserID", types.ScalarAttributeTypeN),
			attr("Username", types.ScalarAttributeTypeS),
			attr("LinkKey", types.ScalarAttributeTypeN),
			attr("CreatedAt", types.ScalarAttributeTypeN),
		},
		KeySchema: []types.KeySchemaElement{key("UUID", types.KeyTypeHash)},
		GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
			gsi(userIDIndex, key("UserID", types.KeyTypeHash)),
			gsi(usernameIndex, key("Username", types.KeyTypeHash)),
			gsi(linkKeyIndex, key("LinkKey", types.KeyTypeHash), key("CreatedAt", types.KeyTypeRange)),
		},
	})
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	waiter := dynamodb.NewTableExistsWaiter(repo.client)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: &repo.table}, time.Minute); err != nil {
		t.Fatalf("wait for table: %v", err)
	}
	t.Cleanup(func() {
		_, err := repo.client.DeleteTable(context.Background(), &dynamodb.DeleteTableInput{TableName: &repo.table})
		if err != nil {
			t.Errorf("delete table: %v", err)
		}
	})
}

// rawItem reads the stored item without the repository's decoding.
func rawItem(t *testing.T, repo *UserRepository, userUUID string) map[string]types.AttributeValue {
	t.Helper()
	out, err := repo.client.GetItem(context.Background(), &dynamodb.GetItemInput{TableName: &repo.table, Key: itemKey(userUUID)})
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	return out.Item
}

func TestUserLifecycle(t *testing.T) {
	repo := newTestRepo(t)

	u, err := repo.CreateUser(42)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.UUID == "" || u.UserID != 42 || u.State != Idle {
		t.Fatalf("unexpected new user: %+v", u)
	}
	if u.LinkKey < 100000 || u.LinkKey > 999999 {
		t.Errorf("LinkKey %d is not a 6-digit number", u.LinkKey)
	}

	t.Run("stored in the existing item format", func(t *testing.T) {
		item := rawItem(t, repo, u.UUID)
		want := map[string]string{"UUID": "S", "UserID": "N", "State": "S", "LinkKey": "N", "CreatedAt": "N"}
		for name, typ := range want {
			if got := attrType(item[name]); got != typ {
				t.Errorf("%s stored as %q, want %q", name, got, typ)
			}
		}
		// Empty fields must be absent; an empty Username would break its index.
		for _, name := range []string{"Username", "ContactUUID", "ReplyMessageID", "Language", "Blacklist", "LastSentAt", "Role"} {
			if _, ok := item[name]; ok {
				t.Errorf("%s should not be stored for a new user", name)
			}
		}
		if n := item["CreatedAt"].(*types.AttributeValueMemberN).Value; n != fmt.Sprint(u.CreatedAt.Unix()) {
			t.Errorf("CreatedAt stored as %s, want unix seconds %d", n, u.CreatedAt.Unix())
		}
	})

	t.Run("read by UUID", func(t *testing.T) {
		got, err := repo.ReadUserByUUID(u.UUID)
		if err != nil {
			t.Fatalf("ReadUserByUUID: %v", err)
		}
		if got.UserID != 42 || got.LinkKey != u.LinkKey || !got.CreatedAt.Equal(u.CreatedAt) || got.State != Idle {
			t.Errorf("got %+v, want %+v", got, u)
		}
		if _, err := repo.ReadUserByUUID("no-such-uuid"); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown UUID: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("read by Telegram user ID", func(t *testing.T) {
		got, err := repo.ReadUserByUserId(42)
		if err != nil || got.UUID != u.UUID {
			t.Fatalf("ReadUserByUserId: %+v, %v", got, err)
		}
		// The bot creates a user only on ErrNotFound.
		if _, err := repo.ReadUserByUserId(43); !errors.Is(err, ErrNotFound) {
			t.Errorf("unknown user ID: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("read by link key", func(t *testing.T) {
		got, err := repo.ReadUserByLinkKey(u.LinkKey, u.CreatedAt.Unix())
		if err != nil || got.UUID != u.UUID {
			t.Fatalf("ReadUserByLinkKey: %+v, %v", got, err)
		}
		if _, err := repo.ReadUserByLinkKey(u.LinkKey, u.CreatedAt.Unix()+1); !errors.Is(err, ErrNotFound) {
			t.Errorf("wrong creation time: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("sending a new message, then a reply, then reset", func(t *testing.T) {
		if err := repo.StartSending(u, "contact-uuid", 0, conversation.Anonymous); err != nil {
			t.Fatalf("StartSending: %v", err)
		}
		assertStored(t, repo, u, func(got *User) bool {
			return got.State == Sending && got.ContactUUID == "contact-uuid" && got.ReplyMessageID == 0 &&
				got.Role == conversation.Anonymous
		})
		item := rawItem(t, repo, u.UUID)
		if _, ok := item["ReplyMessageID"]; ok {
			t.Error("ReplyMessageID should be absent for a new message")
		}
		if typ := attrType(item["Role"]); typ != "S" {
			t.Errorf("Role stored as %q, want S", typ)
		}

		if err := repo.StartSending(u, "contact-uuid", 7, conversation.Known); err != nil {
			t.Fatalf("StartSending reply: %v", err)
		}
		assertStored(t, repo, u, func(got *User) bool {
			return got.State == Sending && got.ContactUUID == "contact-uuid" && got.ReplyMessageID == 7 &&
				got.Role == conversation.Known
		})

		if err := repo.ResetState(u); err != nil {
			t.Fatalf("ResetState: %v", err)
		}
		assertStored(t, repo, u, func(got *User) bool {
			return got.State == Idle && got.ContactUUID == "" && got.ReplyMessageID == 0 && got.Role == ""
		})
		if _, ok := rawItem(t, repo, u.UUID)["Role"]; ok {
			t.Error("Role should be removed when the conversation ends")
		}
	})

	t.Run("sending needs a valid role", func(t *testing.T) {
		if err := repo.StartSending(u, "contact-uuid", 0, ""); err == nil {
			t.Error("StartSending without a role should fail")
		}
		assertStored(t, repo, u, func(got *User) bool { return got.State == Idle && got.Role == "" })
	})

	t.Run("username: start, set, find, remove", func(t *testing.T) {
		if err := repo.StartSending(u, "contact-uuid", 0, conversation.Anonymous); err != nil {
			t.Fatalf("StartSending: %v", err)
		}
		if err := repo.StartSettingUsername(u); err != nil {
			t.Fatalf("StartSettingUsername: %v", err)
		}
		assertStored(t, repo, u, func(got *User) bool {
			return got.State == SettingUsername && got.ContactUUID == "" && got.Role == ""
		})

		if err := repo.SetUsername(u, "alice"); err != nil {
			t.Fatalf("SetUsername: %v", err)
		}
		assertStored(t, repo, u, func(got *User) bool { return got.Username == "alice" && got.State == Idle })
		got, err := repo.ReadUserByUsername("alice")
		if err != nil || got.UUID != u.UUID {
			t.Fatalf("ReadUserByUsername: %+v, %v", got, err)
		}

		if err := repo.SetUsername(u, ""); err == nil {
			t.Error("SetUsername should reject an empty username")
		}

		if err := repo.RemoveUsername(u); err != nil {
			t.Fatalf("RemoveUsername: %v", err)
		}
		assertStored(t, repo, u, func(got *User) bool { return got.Username == "" && got.State == Idle })
		if _, ok := rawItem(t, repo, u.UUID)["Username"]; ok {
			t.Error("Username attribute should be removed, not emptied")
		}
		if _, err := repo.ReadUserByUsername("alice"); !errors.Is(err, ErrNotFound) {
			t.Errorf("removed username still resolves: %v", err)
		}
		if _, err := repo.ReadUserByUsername(""); !errors.Is(err, ErrNotFound) {
			t.Errorf("empty username: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("language", func(t *testing.T) {
		if err := repo.StartSending(u, "contact-uuid", 3, conversation.Known); err != nil {
			t.Fatalf("StartSending: %v", err)
		}
		if err := repo.SetLanguage(u, i18n.FaIR); err != nil {
			t.Fatalf("SetLanguage: %v", err)
		}
		assertStored(t, repo, u, func(got *User) bool {
			return got.Language == i18n.FaIR && got.State == Idle && got.ContactUUID == "" && got.ReplyMessageID == 0 &&
				got.Role == ""
		})
	})

	t.Run("blacklist", func(t *testing.T) {
		for _, id := range []string{"a", "b", "a"} {
			if err := repo.Block(u, id); err != nil {
				t.Fatalf("Block(%s): %v", id, err)
			}
		}
		assertBlacklist(t, repo, u, "a", "b")
		if typ := attrType(rawItem(t, repo, u.UUID)["Blacklist"]); typ != "SS" {
			t.Errorf("Blacklist stored as %q, want a string set", typ)
		}

		if err := repo.Unblock(u, "a"); err != nil {
			t.Fatalf("Unblock: %v", err)
		}
		assertBlacklist(t, repo, u, "b")

		if err := repo.UnblockAll(u); err != nil {
			t.Fatalf("UnblockAll: %v", err)
		}
		assertBlacklist(t, repo, u)
	})

	t.Run("updates never create a user", func(t *testing.T) {
		ghost := &User{UUID: "does-not-exist"}
		if err := repo.ResetState(ghost); err == nil {
			t.Error("updating a missing user should fail")
		}
		if item := rawItem(t, repo, ghost.UUID); item != nil {
			t.Errorf("update created an item: %v", item)
		}
	})
}

// TestFollowUps covers the rules that decide whether a user's next message
// still goes to the same person (see the conversation package).
func TestFollowUps(t *testing.T) {
	repo := newTestRepo(t)
	u, err := repo.CreateUser(7)
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	sentAt := time.Unix(1762600000, 0)
	stored := func(t *testing.T) *User {
		t.Helper()
		got, err := repo.ReadUserByUUID(u.UUID)
		if err != nil {
			t.Fatalf("ReadUserByUUID: %v", err)
		}
		return got
	}
	interrupt := func(t *testing.T, from string, fromRole conversation.Role) bool {
		t.Helper()
		ended, err := repo.InterruptFollowUps(u.UUID, from, fromRole)
		if err != nil {
			t.Fatalf("InterruptFollowUps: %v", err)
		}
		return ended
	}
	// continueWithA puts u in a conversation with contact-a, having opened
	// contact-a's link: u is anonymous and contact-a is known.
	continueWithA := func(t *testing.T) {
		t.Helper()
		if err := repo.StartSending(u, "contact-a", 9, conversation.Anonymous); err != nil {
			t.Fatalf("StartSending: %v", err)
		}
		if err := repo.ContinueConversation(u, sentAt); err != nil {
			t.Fatalf("ContinueConversation: %v", err)
		}
	}
	assertReset := func(t *testing.T) {
		t.Helper()
		got := stored(t)
		if got.State != Idle || got.ContactUUID != "" || got.ReplyMessageID != 0 || got.LastSentAt != 0 || got.Role != "" {
			t.Errorf("not reset: %+v", got)
		}
		item := rawItem(t, repo, u.UUID)
		for _, name := range []string{"LastSentAt", "Role"} {
			if _, ok := item[name]; ok {
				t.Errorf("%s should be removed", name)
			}
		}
	}

	t.Run("continuing keeps the recipient, the thread and the role", func(t *testing.T) {
		continueWithA(t)
		assertStored(t, repo, u, func(got *User) bool {
			return got.State == Continuing && got.ContactUUID == "contact-a" &&
				got.ReplyMessageID == 9 && got.LastSentAt == sentAt.Unix() && got.Role == conversation.Anonymous
		})
		if typ := attrType(rawItem(t, repo, u.UUID)["LastSentAt"]); typ != "N" {
			t.Errorf("LastSentAt stored as %q, want N", typ)
		}
	})

	t.Run("a message in the same conversation doesn't interrupt", func(t *testing.T) {
		if interrupt(t, "contact-a", conversation.Known) {
			t.Error("follow-ups ended by a message from the same conversation")
		}
		if got := stored(t); got.State != Continuing || got.ContactUUID != "contact-a" {
			t.Errorf("stored user changed: %+v", got)
		}
	})

	t.Run("the same person in their other conversation interrupts", func(t *testing.T) {
		// contact-a opened u's link too, and writes anonymously: to u that's
		// someone else, so it must behave like someone else.
		if !interrupt(t, "contact-a", conversation.Anonymous) {
			t.Error("follow-ups not ended by a message from the other conversation")
		}
		assertReset(t)
	})

	t.Run("a message from someone else interrupts", func(t *testing.T) {
		continueWithA(t)
		if !interrupt(t, "contact-b", conversation.Known) {
			t.Error("follow-ups not ended by a message from someone else")
		}
		assertReset(t)
	})

	t.Run("a conversation without a role is interrupted by anyone", func(t *testing.T) {
		// Conversations started before roles existed.
		legacy := expression.Set(expression.Name("State"), expression.Value(Continuing)).
			Set(expression.Name("ContactUUID"), expression.Value("contact-a")).
			Remove(expression.Name("Role"))
		if err := repo.update(u, legacy); err != nil {
			t.Fatalf("update: %v", err)
		}
		if !interrupt(t, "contact-a", conversation.Known) {
			t.Error("a conversation without a role was not interrupted")
		}
		assertReset(t)
	})

	t.Run("an explicit reply in progress isn't interrupted", func(t *testing.T) {
		if err := repo.StartSending(u, "contact-a", 5, conversation.Known); err != nil {
			t.Fatalf("StartSending: %v", err)
		}
		if interrupt(t, "contact-b", conversation.Anonymous) {
			t.Error("explicit Sending state was interrupted")
		}
		if got := stored(t); got.State != Sending || got.ContactUUID != "contact-a" || got.ReplyMessageID != 5 {
			t.Errorf("stored user changed: %+v", got)
		}
	})

	t.Run("idle and username states aren't affected", func(t *testing.T) {
		if err := repo.ResetState(u); err != nil {
			t.Fatalf("ResetState: %v", err)
		}
		if interrupt(t, "contact-b", conversation.Anonymous) {
			t.Error("idle user reported as interrupted")
		}
		if err := repo.StartSettingUsername(u); err != nil {
			t.Fatalf("StartSettingUsername: %v", err)
		}
		if interrupt(t, "contact-b", conversation.Anonymous) || stored(t).State != SettingUsername {
			t.Error("setting-username state was interrupted")
		}
	})

	t.Run("explicit states and resets clear the follow-up timestamp", func(t *testing.T) {
		for name, next := range map[string]func() error{
			"StartSending": func() error { return repo.StartSending(u, "contact-c", 0, conversation.Anonymous) },
			"ResetState":   func() error { return repo.ResetState(u) },
		} {
			if err := repo.ContinueConversation(u, sentAt); err != nil {
				t.Fatalf("ContinueConversation: %v", err)
			}
			if err := next(); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			if u.LastSentAt != 0 || stored(t).LastSentAt != 0 {
				t.Errorf("%s left LastSentAt set", name)
			}
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		ended, err := repo.InterruptFollowUps("no-such-uuid", "contact-b", conversation.Known)
		if err != nil || ended {
			t.Errorf("InterruptFollowUps(unknown) = %v, %v; want false, nil", ended, err)
		}
		if item := rawItem(t, repo, "no-such-uuid"); item != nil {
			t.Errorf("created an item: %v", item)
		}
	})
}

// TestReadsExistingItemFormat checks that items stored by the previous
// implementation (the format currently in production) still decode.
func TestReadsExistingItemFormat(t *testing.T) {
	repo := newTestRepo(t)
	created := time.Unix(1762600000, 0)
	item := map[string]types.AttributeValue{
		"UUID":           &types.AttributeValueMemberS{Value: "legacy-uuid"},
		"UserID":         &types.AttributeValueMemberN{Value: "1001"},
		"State":          &types.AttributeValueMemberS{Value: "IDLE"},
		"LinkKey":        &types.AttributeValueMemberN{Value: "123456"},
		"CreatedAt":      &types.AttributeValueMemberN{Value: fmt.Sprint(created.Unix())},
		"ReplyMessageID": &types.AttributeValueMemberN{Value: "0"},
		"Username":       &types.AttributeValueMemberS{Value: "legacy"},
		"Language":       &types.AttributeValueMemberS{Value: "fa_IR"},
		"Blacklist":      &types.AttributeValueMemberSS{Value: []string{"x", "y"}},
	}
	if _, err := repo.client.PutItem(context.Background(), &dynamodb.PutItemInput{TableName: &repo.table, Item: item}); err != nil {
		t.Fatalf("PutItem: %v", err)
	}

	for name, read := range map[string]func() (*User, error){
		"by UUID":     func() (*User, error) { return repo.ReadUserByUUID("legacy-uuid") },
		"by user ID":  func() (*User, error) { return repo.ReadUserByUserId(1001) },
		"by username": func() (*User, error) { return repo.ReadUserByUsername("legacy") },
		"by link key": func() (*User, error) { return repo.ReadUserByLinkKey(123456, created.Unix()) },
	} {
		got, err := read()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got.UUID != "legacy-uuid" || got.UserID != 1001 || got.State != Idle || got.LinkKey != 123456 ||
			!got.CreatedAt.Equal(created) || got.ReplyMessageID != 0 || got.Username != "legacy" ||
			got.Language != i18n.FaIR || !slices.Equal(sorted(got.Blacklist), []string{"x", "y"}) {
			t.Errorf("%s: decoded %+v", name, got)
		}
	}
}

// assertStored checks a condition on both the in-memory user and the stored item.
func assertStored(t *testing.T, repo *UserRepository, u *User, ok func(*User) bool) {
	t.Helper()
	stored, err := repo.ReadUserByUUID(u.UUID)
	if err != nil {
		t.Fatalf("ReadUserByUUID: %v", err)
	}
	if !ok(u) {
		t.Errorf("in-memory user: %+v", u)
	}
	if !ok(stored) {
		t.Errorf("stored user: %+v", stored)
	}
}

func assertBlacklist(t *testing.T, repo *UserRepository, u *User, want ...string) {
	t.Helper()
	stored, err := repo.ReadUserByUUID(u.UUID)
	if err != nil {
		t.Fatalf("ReadUserByUUID: %v", err)
	}
	for label, list := range map[string][]string{"in-memory": u.Blacklist, "stored": stored.Blacklist} {
		if got := sorted(list); !slices.Equal(got, want) && !(len(got) == 0 && len(want) == 0) {
			t.Errorf("%s blacklist = %v, want %v", label, got, want)
		}
	}
}

func sorted(s []string) []string {
	s = slices.Clone(s)
	slices.Sort(s)
	return s
}

func attrType(av types.AttributeValue) string {
	switch av.(type) {
	case *types.AttributeValueMemberS:
		return "S"
	case *types.AttributeValueMemberN:
		return "N"
	case *types.AttributeValueMemberSS:
		return "SS"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%T", av)
	}
}
