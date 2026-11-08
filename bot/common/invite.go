package common

// Invite-only access: the bot-facing side of the invitations package. All
// invitation handlers live in this file; the only other hooks are in start
// (invitation links), getLink and the username flow (who may own a link), and
// processText (the two invitation prompts). See the invitations package for
// how to open the bot to everyone or remove the system.

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"strconv"
	"strings"

	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"github.com/bugfloyd/telegram-incognito-bot/common/i18n"
	"github.com/bugfloyd/telegram-incognito-bot/common/invitations"
	"github.com/bugfloyd/telegram-incognito-bot/common/users"
)

// adminMaxUses caps how many people a single admin invitation can register.
const adminMaxUses = 1000

// listedCodes is how many of a user's most recent codes /invite shows.
const listedCodes = 20

const (
	createInvitationCallbackData = "inv|c"
	cancelInvitationCallbackData = "inv|x"
)

func (r *RootHandler) isAdmin(ctx *ext.Context) bool {
	return invitations.IsAdmin(ctx.EffectiveUser.Id)
}

// mayOwnLink checks whether the user may have their own link (and so receive
// messages). If not, it explains that the bot is invite-only.
func (r *RootHandler) mayOwnLink(b *gotgbot.Bot, ctx *ext.Context) (bool, error) {
	if invitations.CanOwnLink(r.user.Registered(), ctx.EffectiveUser.Id) {
		return true, nil
	}
	if _, err := ctx.EffectiveMessage.Reply(b, i18n.T(i18n.InviteOnlyText), nil); err != nil {
		return false, fmt.Errorf("failed to send invite-only notice: %w", err)
	}
	return false, nil
}

func cancelKeyboard() gotgbot.InlineKeyboardMarkup {
	return gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
		{Text: i18n.T(i18n.CancelButtonText), CallbackData: cancelInvitationCallbackData},
	}}}
}

func (r *RootHandler) reply(b *gotgbot.Bot, ctx *ext.Context, text string, opts *gotgbot.SendMessageOpts) error {
	if _, err := ctx.EffectiveMessage.Reply(b, text, opts); err != nil {
		return fmt.Errorf("failed to reply: %w", err)
	}
	return nil
}

// alreadyIn answers users who don't need an invitation (bot open, admin, or
// registered). It reports whether it answered.
func (r *RootHandler) alreadyIn(b *gotgbot.Bot, ctx *ext.Context) (bool, error) {
	switch {
	case !invitations.InviteOnly():
		return true, r.reply(b, ctx, i18n.T(i18n.InvitationNotNeededText), nil)
	case r.user.Registered() || r.isAdmin(ctx):
		return true, r.reply(b, ctx, i18n.T(i18n.AlreadyRegisteredText), nil)
	}
	return false, nil
}

// ---------------------------------------------------------------------------
// Joining: /register, invitation links, and the code prompt
// ---------------------------------------------------------------------------

func (r *RootHandler) register(b *gotgbot.Bot, ctx *ext.Context) error {
	if answered, err := r.alreadyIn(b, ctx); answered || err != nil {
		return err
	}
	if args := ctx.Args(); len(args) > 1 { // /register <code>
		return r.redeemInvitation(b, ctx, strings.Join(args[1:], " "))
	}
	if err := r.userRepo.StartEnteringInvitationCode(r.user); err != nil {
		return err
	}
	return r.reply(b, ctx, i18n.T(i18n.EnterInvitationCodeText), &gotgbot.SendMessageOpts{ReplyMarkup: cancelKeyboard()})
}

// startWithInvitation handles t.me/<bot>?start=inv-<code> links.
func (r *RootHandler) startWithInvitation(b *gotgbot.Bot, ctx *ext.Context, code string) error {
	if answered, err := r.alreadyIn(b, ctx); answered || err != nil {
		return err
	}
	return r.redeemInvitation(b, ctx, code)
}

// redeemInvitation registers the user with a code typed, pasted or linked.
// Invalid or used-up codes keep the prompt open so the user can try again.
func (r *RootHandler) redeemInvitation(b *gotgbot.Bot, ctx *ext.Context, input string) error {
	if answered, err := r.alreadyIn(b, ctx); answered || err != nil {
		if err == nil && r.user.State == users.EnteringInvitationCode {
			err = r.userRepo.ResetState(r.user)
		}
		return err
	}

	retry := func(textID i18n.TextID) error {
		if r.user.State != users.EnteringInvitationCode {
			if err := r.userRepo.StartEnteringInvitationCode(r.user); err != nil {
				return err
			}
		}
		return r.reply(b, ctx, fmt.Sprintf(i18n.T(textID), i18n.T(i18n.CancelButtonText)),
			&gotgbot.SendMessageOpts{ReplyMarkup: cancelKeyboard()})
	}

	code, ok := invitations.NormalizeCode(input)
	if !ok {
		return retry(i18n.InvalidInvitationCodeText)
	}
	repo, err := invitations.NewRepository()
	if err != nil {
		return err
	}
	_, err = repo.RedeemCode(code, r.user)
	switch {
	case errors.Is(err, invitations.ErrCodeNotFound):
		return retry(i18n.InvalidInvitationCodeText)
	case errors.Is(err, invitations.ErrCodeUsedUp):
		return retry(i18n.InvitationUsedUpText)
	case errors.Is(err, invitations.ErrAlreadyRegistered):
		if err := r.userRepo.ResetState(r.user); err != nil {
			return err
		}
		return r.reply(b, ctx, i18n.T(i18n.AlreadyRegisteredText), nil)
	case err != nil:
		return err
	}
	if r.user.State != users.Idle {
		if err := r.userRepo.ResetState(r.user); err != nil {
			return err
		}
	}
	return r.reply(b, ctx, i18n.T(i18n.RegisteredText), nil)
}

// ---------------------------------------------------------------------------
// Inviting: /invite, the create button and the "how many people" prompt
// ---------------------------------------------------------------------------

// invitationsLeft returns how many people the user can still invite, or
// unlimited for admins. ok is false if the user can't invite at all.
func (r *RootHandler) invitationsLeft(ctx *ext.Context, repo *invitations.Repository) (left int, unlimited, ok bool, err error) {
	if r.isAdmin(ctx) {
		return adminMaxUses, true, true, nil
	}
	if !r.user.Registered() {
		return 0, false, false, nil
	}
	left, err = repo.Remaining(r.user)
	return left, false, true, err
}

func (r *RootHandler) invite(b *gotgbot.Bot, ctx *ext.Context) error {
	if !invitations.InviteOnly() {
		return r.reply(b, ctx, i18n.T(i18n.InvitationNotNeededText), nil)
	}
	repo, err := invitations.NewRepository()
	if err != nil {
		return err
	}
	left, unlimited, ok, err := r.invitationsLeft(ctx, repo)
	if err != nil {
		return err
	}
	if !ok {
		return r.reply(b, ctx, i18n.T(i18n.InviteOnlyText), nil)
	}

	var text strings.Builder
	if unlimited {
		text.WriteString(html.EscapeString(i18n.T(i18n.UnlimitedInvitationsText)))
	} else {
		text.WriteString(html.EscapeString(fmt.Sprintf(i18n.T(i18n.InvitationsLeftText), left)))
	}
	codes, err := repo.ListCodes(r.user.UUID, listedCodes)
	if err != nil {
		return err
	}
	if len(codes) > 0 {
		text.WriteString("\n\n" + html.EscapeString(i18n.T(i18n.YourInvitationsText)))
		for _, code := range codes {
			line := fmt.Sprintf(html.EscapeString(i18n.T(i18n.InvitationLineText)),
				"<code>"+html.EscapeString(code.Code())+"</code>", code.UsesTotal-code.UsesLeft, code.UsesTotal)
			text.WriteString("\n" + line)
		}
	}

	opts := &gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML}
	if left > 0 {
		opts.ReplyMarkup = gotgbot.InlineKeyboardMarkup{InlineKeyboard: [][]gotgbot.InlineKeyboardButton{{
			{Text: i18n.T(i18n.CreateInvitationButtonText), CallbackData: createInvitationCallbackData},
		}}}
	}
	return r.reply(b, ctx, text.String(), opts)
}

func (r *RootHandler) createInvitationCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	if _, err := ctx.Update.CallbackQuery.Answer(b, nil); err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}
	if !invitations.InviteOnly() {
		return r.reply(b, ctx, i18n.T(i18n.InvitationNotNeededText), nil)
	}
	repo, err := invitations.NewRepository()
	if err != nil {
		return err
	}
	left, _, ok, err := r.invitationsLeft(ctx, repo)
	if err != nil {
		return err
	}
	if !ok || left < 1 {
		return r.reply(b, ctx, i18n.T(i18n.NoInvitationsToGiveText), nil)
	}
	if err := r.userRepo.StartCreatingInvitation(r.user); err != nil {
		return err
	}
	return r.reply(b, ctx, fmt.Sprintf(i18n.T(i18n.HowManyUsesText), left), &gotgbot.SendMessageOpts{ReplyMarkup: cancelKeyboard()})
}

// createInvitation handles the answer to "how many people".
func (r *RootHandler) createInvitation(b *gotgbot.Bot, ctx *ext.Context) error {
	repo, err := invitations.NewRepository()
	if err != nil {
		return err
	}
	left, unlimited, ok, err := r.invitationsLeft(ctx, repo)
	if err != nil {
		return err
	}
	if !invitations.InviteOnly() || !ok || left < 1 {
		if err := r.userRepo.ResetState(r.user); err != nil {
			return err
		}
		if !invitations.InviteOnly() {
			return r.reply(b, ctx, i18n.T(i18n.InvitationNotNeededText), nil)
		}
		return r.reply(b, ctx, i18n.T(i18n.NoInvitationsToGiveText), nil)
	}

	uses, err := strconv.Atoi(strings.TrimSpace(ctx.EffectiveMessage.Text))
	if err != nil || uses < 1 || uses > left {
		return r.reply(b, ctx, fmt.Sprintf(i18n.T(i18n.InvalidUsesText), left), &gotgbot.SendMessageOpts{ReplyMarkup: cancelKeyboard()})
	}

	code, err := repo.CreateCode(r.user, uses, unlimited)
	if errors.Is(err, invitations.ErrNoInvitationsLeft) || errors.Is(err, invitations.ErrNotRegistered) {
		// Spent elsewhere in the meantime.
		if err := r.userRepo.ResetState(r.user); err != nil {
			return err
		}
		return r.reply(b, ctx, i18n.T(i18n.NoInvitationsToGiveText), nil)
	}
	if err != nil {
		return err
	}
	if err := r.userRepo.ResetState(r.user); err != nil {
		return err
	}
	link := fmt.Sprintf("https://t.me/%s?start=%s", b.User.Username, invitations.LinkPayload(code.Code()))
	text := fmt.Sprintf(html.EscapeString(i18n.T(i18n.InvitationCreatedText)),
		uses, html.EscapeString(link), "<code>"+html.EscapeString(code.Code())+"</code>")
	return r.reply(b, ctx, text, &gotgbot.SendMessageOpts{ParseMode: gotgbot.ParseModeHTML})
}

// cancelInvitationCallback ends the code or "how many" prompt. The callback
// router has already reset the user's state.
func (r *RootHandler) cancelInvitationCallback(b *gotgbot.Bot, ctx *ext.Context) error {
	cb := ctx.Update.CallbackQuery
	if _, _, err := cb.Message.EditReplyMarkup(b, &gotgbot.EditMessageReplyMarkupOpts{}); err != nil {
		return fmt.Errorf("failed to remove buttons: %w", err)
	}
	if _, err := cb.Answer(b, &gotgbot.AnswerCallbackQueryOpts{Text: i18n.T(i18n.NeverMindButtonText)}); err != nil {
		return fmt.Errorf("failed to answer callback: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Admin commands (English only). Non-admins get the usual "Invalid command!",
// so these commands aren't advertised to anyone else.
// ---------------------------------------------------------------------------

func (r *RootHandler) adminOnly(b *gotgbot.Bot, ctx *ext.Context) (bool, error) {
	if r.isAdmin(ctx) {
		return true, nil
	}
	return false, r.sendError(b, ctx, i18n.T(i18n.InvalidCommandText))
}

// levels shows each level's invitation allowance: /levels
func (r *RootHandler) levels(b *gotgbot.Bot, ctx *ext.Context) error {
	if ok, err := r.adminOnly(b, ctx); !ok {
		return err
	}
	repo, err := invitations.NewRepository()
	if err != nil {
		return err
	}
	allowances, err := repo.LevelAllowances()
	if err != nil {
		return err
	}
	var text strings.Builder
	text.WriteString("Invitation allowance per level (unlisted levels: 0):")
	maxLevel := 0
	for level := range allowances {
		maxLevel = max(maxLevel, level)
	}
	for level := 1; level <= maxLevel; level++ {
		fmt.Fprintf(&text, "\nLevel %d: %d", level, allowances[level])
	}
	if maxLevel == 0 {
		text.WriteString("\n(none set)")
	}
	fmt.Fprintf(&text, "\n\nInvite-only: %v\nChange with /setlevel <level> <invitations>, e.g. /setlevel 1 5", invitations.InviteOnly())
	return r.reply(b, ctx, text.String(), nil)
}

// setLevel changes a level's allowance for existing and new users: /setlevel <level> <invitations>
func (r *RootHandler) setLevel(b *gotgbot.Bot, ctx *ext.Context) error {
	if ok, err := r.adminOnly(b, ctx); !ok {
		return err
	}
	args := ctx.Args()
	var level, n int
	var err1, err2 error
	if len(args) == 3 {
		level, err1 = strconv.Atoi(args[1])
		n, err2 = strconv.Atoi(args[2])
	}
	if len(args) != 3 || err1 != nil || err2 != nil || level < 1 || n < 0 {
		return r.reply(b, ctx, "Usage: /setlevel <level> <invitations>\nExample: /setlevel 2 3 gives every level-2 user 3 invitations (level 1 = people invited by an admin).", nil)
	}
	repo, err := invitations.NewRepository()
	if err != nil {
		return err
	}
	if err := repo.SetLevelAllowance(level, n); err != nil {
		return err
	}
	return r.reply(b, ctx, fmt.Sprintf("Level %d allowance is now %d. It applies right away to existing and new level-%d users.", level, n, level), nil)
}

// grant gives one user extra invitations: /grant <@username or link> <invitations>
func (r *RootHandler) grant(b *gotgbot.Bot, ctx *ext.Context) error {
	if ok, err := r.adminOnly(b, ctx); !ok {
		return err
	}
	const usage = "Usage: /grant <@username or their link> <invitations>\nExample: /grant @alice 5"
	args := ctx.Args()
	if len(args) != 3 {
		return r.reply(b, ctx, usage, nil)
	}
	n, err := strconv.Atoi(args[2])
	if err != nil || n < 1 {
		return r.reply(b, ctx, usage, nil)
	}
	target, err := r.resolveUserReference(args[1])
	if err != nil {
		return r.reply(b, ctx, "No user found for that username or link.", nil)
	}
	repo, err := invitations.NewRepository()
	if err != nil {
		return err
	}
	if err := repo.Grant(target, n); errors.Is(err, invitations.ErrNotRegistered) {
		return r.reply(b, ctx, "That user isn't registered yet, so they can't invite anyone.", nil)
	} else if err != nil {
		return err
	}
	left, err := repo.Remaining(target)
	if err != nil {
		return err
	}
	return r.reply(b, ctx, fmt.Sprintf("Granted %d invitations to that user (level %d). They can now give %d.", n, target.Level, left), nil)
}

// resolveUserReference finds a user from what they'd share: @username,
// username, or their t.me link (username or generic).
func (r *RootHandler) resolveUserReference(ref string) (*users.User, error) {
	ref = strings.TrimSpace(ref)
	if u, err := url.Parse(ref); err == nil && u.Host != "" {
		ref = u.Query().Get("start")
	}
	switch {
	case strings.HasPrefix(ref, "@"):
		return r.userRepo.ReadUserByUsername(strings.ToLower(ref[1:]))
	case strings.HasPrefix(ref, "_"):
		return r.userRepo.ReadUserByUsername(strings.ToLower(ref[1:]))
	}
	if user, err := r.userRepo.ReadUserByUsername(strings.ToLower(ref)); err == nil {
		return user, nil
	}
	linkKey, createdAt, err := readUserLinkKey(ref)
	if err != nil {
		return nil, users.ErrNotFound
	}
	return r.userRepo.ReadUserByLinkKey(linkKey, createdAt)
}
