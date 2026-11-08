// Package invitations implements invite-only access.
//
// While the bot is invite-only (INVITE_ONLY, on unless set to "false"), users
// need an invitation to get their own link or username; anyone can still send
// messages to registered users. Users join a "level": people invited by an
// admin are level 1, their invitees level 2, and so on. Each level has an
// allowance of invitations that admins can change at any time, and admins can
// grant individual users extra invitations.
//
// Setting INVITE_ONLY=false opens the bot to everyone without code changes.
// To remove the system entirely, delete this package, common/invite.go, the
// hooks that call them, and the invitations table.
package invitations

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

// InviteOnly reports whether users need an invitation to receive messages.
func InviteOnly() bool {
	return parseInviteOnly(os.Getenv("INVITE_ONLY"))
}

// parseInviteOnly defaults to invite-only: only an explicit "false" opens the bot.
func parseInviteOnly(value string) bool {
	return !strings.EqualFold(strings.TrimSpace(value), "false")
}

var adminIDs = sync.OnceValue(func() map[int64]bool {
	return parseAdminIDs(os.Getenv("ADMIN_TELEGRAM_IDS"))
})

// IsAdmin reports whether a Telegram user ID belongs to an admin, as listed in
// ADMIN_TELEGRAM_IDS (comma-separated).
func IsAdmin(telegramUserID int64) bool {
	return adminIDs()[telegramUserID]
}

func parseAdminIDs(value string) map[int64]bool {
	ids := map[int64]bool{}
	for _, part := range strings.Split(value, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && id > 0 {
			ids[id] = true
		}
	}
	return ids
}

// CanOwnLink reports whether a user may have their own link (and so receive
// messages). Admins and registered users always can.
func CanOwnLink(registered bool, telegramUserID int64) bool {
	return !InviteOnly() || registered || IsAdmin(telegramUserID)
}

// Remaining is how many invitation uses a user can still put into codes.
func Remaining(levelAllowance, granted, allocated int) int {
	return max(0, levelAllowance+granted-allocated)
}
