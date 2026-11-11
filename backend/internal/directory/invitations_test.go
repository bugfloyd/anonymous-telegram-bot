package directory

import (
	"context"
	"errors"
	"testing"

	"github.com/incognitochat/incognitochat/backend/internal/links"
)

func TestLevels(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	if levels, err := s.Levels(ctx); err != nil || len(levels) != 0 {
		t.Fatalf("Levels = %v, %v", levels, err)
	}
	if err := s.SetLevel(ctx, 1, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLevel(ctx, 2, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.SetLevel(ctx, 1, 4); err != nil {
		t.Fatal(err)
	}
	if levels, err := s.Levels(ctx); err != nil || len(levels) != 2 || levels[1] != 4 || levels[2] != 3 {
		t.Fatalf("Levels = %v, %v", levels, err)
	}
}

func TestInvitations(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	admin := registered(t, s)
	alice := registered(t, s)
	bob := registered(t, s)
	carol := registered(t, s)

	// An admin's code makes level-1 members.
	if err := s.CreateInvitation(ctx, admin.ID, "abcd-efgh", 2, 0, -1); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInvitation(ctx, alice.ID, "abcd-efgh", 1, 0, -1); !errors.Is(err, ErrConflict) {
		t.Fatalf("taken code: %v", err)
	}
	if list, err := s.Invitations(ctx, admin.ID); err != nil || len(list) != 1 || list[0] != (Invitation{Code: "abcd-efgh", Uses: 2, Left: 2}) {
		t.Fatalf("Invitations = %+v, %v", list, err)
	}
	// Not by the account that made it, and that uses nothing up.
	if _, err := s.Redeem(ctx, admin.ID, "abcd-efgh"); !errors.Is(err, ErrOwnInvitation) {
		t.Fatalf("own code: %v", err)
	}
	if list, _ := s.Invitations(ctx, admin.ID); list[0].Left != 2 {
		t.Fatalf("own redeem used the code: %+v", list)
	}
	if level, err := s.Redeem(ctx, alice.ID, "abcd-efgh"); err != nil || level != 1 {
		t.Fatalf("Redeem = %d, %v", level, err)
	}
	if _, err := s.Redeem(ctx, alice.ID, "abcd-efgh"); !errors.Is(err, ErrAlreadyMember) {
		t.Fatalf("second redeem: %v", err)
	}
	if _, err := s.Redeem(ctx, bob.ID, "abcd-efgh"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redeem(ctx, carol.ID, "abcd-efgh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("used-up code: %v", err)
	}
	if _, err := s.Redeem(ctx, carol.ID, "zzzz-zzzz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown code: %v", err)
	}
	if list, _ := s.Invitations(ctx, admin.ID); list[0].Left != 0 {
		t.Fatalf("uses left = %d", list[0].Left)
	}
	dev, _ := s.Device(ctx, alice.ID, alice.DeviceID, now)
	if !dev.Member || dev.Level != 1 {
		t.Fatalf("alice = %+v", dev)
	}

	// A member spends from their allowance (here 2): codes for 2 uses, then no more.
	if err := s.CreateInvitation(ctx, alice.ID, "aaaa-aaaa", 2, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateInvitation(ctx, alice.ID, "bbbb-bbbb", 1, 1, 2); !errors.Is(err, ErrLimit) {
		t.Fatalf("over allowance: %v", err)
	}
	if level, err := s.Redeem(ctx, carol.ID, "aaaa-aaaa"); err != nil || level != 2 {
		t.Fatalf("level-2 redeem = %d, %v", level, err)
	}
	// Non-members have no allowance at all.
	if err := s.CreateInvitation(ctx, admin.ID, "cccc-cccc", 1, 0, 5); !errors.Is(err, ErrLimit) {
		t.Fatalf("non-member with an allowance: %v", err)
	}
}

func TestGrant(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	admin := registered(t, s)
	member := registered(t, s)
	other := registered(t, s)
	if err := s.CreateInvitation(ctx, admin.ID, "abcd-efgh", 1, 0, -1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redeem(ctx, member.ID, "abcd-efgh"); err != nil {
		t.Fatal(err)
	}
	memberLink, otherLink := testLink(t), testLink(t)
	if err := s.CreateLink(ctx, member.ID, memberLink, links.MaxActive); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateLink(ctx, other.ID, otherLink, links.MaxActive); err != nil {
		t.Fatal(err)
	}

	if err := s.Grant(ctx, memberLink.ID, 3); err != nil {
		t.Fatal(err)
	}
	if err := s.Grant(ctx, memberLink.ID, 2); err != nil {
		t.Fatal(err)
	}
	if dev, _ := s.Device(ctx, member.ID, member.DeviceID, now); dev.Granted != 5 {
		t.Fatalf("granted = %d", dev.Granted)
	}
	if err := s.Grant(ctx, otherLink.ID, 1); !errors.Is(err, ErrNotMember) {
		t.Fatalf("grant to a non-member: %v", err)
	}
	if err := s.Grant(ctx, testLink(t).ID, 1); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant through an unknown link: %v", err)
	}
}

func TestReplaceEndsCodes(t *testing.T) {
	s, _ := store(t)
	ctx := context.Background()
	first := registered(t, s)
	if err := s.CreateInvitation(ctx, first.ID, "abcd-efgh", 1, 0, -1); err != nil {
		t.Fatal(err)
	}
	old, _ := s.ByTelegram(ctx, first.TelegramHash)
	if err := s.Replace(ctx, old, newAccount(t, first.TelegramHash), now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Redeem(ctx, registered(t, s).ID, "abcd-efgh"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("code of a replaced account: %v", err)
	}
}
