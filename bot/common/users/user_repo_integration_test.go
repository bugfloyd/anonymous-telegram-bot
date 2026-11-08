package users

import (
	"context"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/guregu/dynamo/v2"
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
	db, err := loadDB()
	if err != nil {
		t.Fatalf("load db: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	// Same shape as infra/main.tf: on-demand, three GSIs projecting ALL.
	name := fmt.Sprintf("%s-test-%d", TableName, time.Now().UnixNano())
	err = db.CreateTable(name, User{}).
		OnDemand(true).
		Project("UserID-GSI", dynamo.AllProjection).
		Project("Username-GSI", dynamo.AllProjection).
		Project("LinkKey-GSI", dynamo.AllProjection).
		Wait(ctx)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Table(name).DeleteTable().Run(context.Background()); err != nil {
			t.Errorf("delete table: %v", err)
		}
	})

	repo, err := newUserRepository(name)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	return repo
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

	t.Run("read by UUID", func(t *testing.T) {
		got, err := repo.ReadUserByUUID(u.UUID)
		if err != nil {
			t.Fatalf("ReadUserByUUID: %v", err)
		}
		if got.UserID != 42 || got.LinkKey != u.LinkKey || got.CreatedAt.Unix() != u.CreatedAt.Unix() {
			t.Errorf("got %+v, want %+v", got, u)
		}
	})

	t.Run("read by Telegram user ID", func(t *testing.T) {
		got, err := repo.ReadUserByUserId(42)
		if err != nil || got.UUID != u.UUID {
			t.Fatalf("ReadUserByUserId: %+v, %v", got, err)
		}
		// The bot creates a new user when this lookup fails, so an unknown
		// ID must return an error.
		if _, err := repo.ReadUserByUserId(43); err == nil {
			t.Error("expected an error for an unknown user ID")
		}
	})

	t.Run("read by link key", func(t *testing.T) {
		got, err := repo.ReadUserByLinkKey(u.LinkKey, u.CreatedAt.Unix())
		if err != nil || got.UUID != u.UUID {
			t.Fatalf("ReadUserByLinkKey: %+v, %v", got, err)
		}
		if _, err := repo.ReadUserByLinkKey(u.LinkKey, u.CreatedAt.Unix()+1); err == nil {
			t.Error("expected an error for a wrong creation time")
		}
	})

	t.Run("update persists and mirrors in memory", func(t *testing.T) {
		err := repo.UpdateUser(u, map[string]interface{}{
			"Username": "alice",
			"State":    SettingUsername,
			"Language": i18n.FaIR,
		})
		if err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		if u.Username != "alice" || u.State != SettingUsername || u.Language != i18n.FaIR {
			t.Errorf("in-memory user not updated: %+v", u)
		}
		got, err := repo.ReadUserByUsername("alice")
		if err != nil || got.UUID != u.UUID || got.State != SettingUsername || got.Language != i18n.FaIR {
			t.Fatalf("ReadUserByUsername: %+v, %v", got, err)
		}
	})

	t.Run("removing the username", func(t *testing.T) {
		if err := repo.UpdateUser(u, map[string]interface{}{"Username": ""}); err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		if _, err := repo.ReadUserByUsername("alice"); err == nil {
			t.Error("username should no longer resolve")
		}
	})

	t.Run("sending state and reset", func(t *testing.T) {
		err := repo.UpdateUser(u, map[string]interface{}{
			"State":          Sending,
			"ContactUUID":    "contact-uuid",
			"ReplyMessageID": int64(7),
		})
		if err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		got, _ := repo.ReadUserByUUID(u.UUID)
		if got.State != Sending || got.ContactUUID != "contact-uuid" || got.ReplyMessageID != 7 {
			t.Fatalf("state not persisted: %+v", got)
		}

		if err := repo.ResetUserState(u); err != nil {
			t.Fatalf("ResetUserState: %v", err)
		}
		got, _ = repo.ReadUserByUUID(u.UUID)
		for _, user := range []*User{u, got} {
			if user.State != Idle || user.ContactUUID != "" || user.ReplyMessageID != 0 {
				t.Errorf("state not reset: %+v", user)
			}
		}
	})

	t.Run("blacklist", func(t *testing.T) {
		for _, step := range []struct{ method, value string }{{"add", "a"}, {"add", "b"}, {"add", "a"}} {
			if err := repo.UpdateBlacklist(u, step.method, step.value); err != nil {
				t.Fatalf("UpdateBlacklist(%s, %s): %v", step.method, step.value, err)
			}
		}
		assertBlacklist(t, repo, u, "a", "b")

		if err := repo.UpdateBlacklist(u, "delete", "a"); err != nil {
			t.Fatalf("delete: %v", err)
		}
		assertBlacklist(t, repo, u, "b")

		if err := repo.UpdateBlacklist(u, "clear", ""); err != nil {
			t.Fatalf("clear: %v", err)
		}
		assertBlacklist(t, repo, u)

		if err := repo.UpdateBlacklist(u, "bogus", "x"); err == nil {
			t.Error("expected an error for an invalid method")
		}
	})
}

// assertBlacklist checks both the in-memory user and the stored item.
func assertBlacklist(t *testing.T, repo *UserRepository, u *User, want ...string) {
	t.Helper()
	got, err := repo.ReadUserByUUID(u.UUID)
	if err != nil {
		t.Fatalf("ReadUserByUUID: %v", err)
	}
	for _, list := range [][]string{u.Blacklist, got.Blacklist} {
		sorted := slices.Clone(list)
		slices.Sort(sorted)
		if !slices.Equal(sorted, want) && !(len(sorted) == 0 && len(want) == 0) {
			t.Errorf("blacklist = %v, want %v", sorted, want)
		}
	}
}
