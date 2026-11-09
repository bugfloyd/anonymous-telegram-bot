package notification

import (
	"errors"
	"fmt"
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

type fakeMessage struct {
	deleteErr, editErr error
	deletes            int
	edits              []*gotgbot.EditMessageTextOpts
}

func (f *fakeMessage) Delete(*gotgbot.Bot, *gotgbot.DeleteMessageOpts) (bool, error) {
	f.deletes++
	return f.deleteErr == nil, f.deleteErr
}

func (f *fakeMessage) EditText(_ *gotgbot.Bot, opts *gotgbot.EditMessageTextOpts) (*gotgbot.Message, bool, error) {
	f.edits = append(f.edits, opts)
	return nil, f.editErr == nil, f.editErr
}

var (
	errTooOld   = errors.New("Bad Request: message can't be deleted for everyone")
	errEditFail = errors.New("Bad Request: message to edit not found")
)

func TestRetireDeletesWhenPossible(t *testing.T) {
	msg := &fakeMessage{}
	if err := Retire(nil, msg, "Opened"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if msg.deletes != 1 || len(msg.edits) != 0 {
		t.Errorf("deletes=%d edits=%d, want 1 delete and no edit", msg.deletes, len(msg.edits))
	}
}

func TestRetireEditsWhenDeleteFails(t *testing.T) {
	msg := &fakeMessage{deleteErr: errTooOld}
	if err := Retire(nil, msg, "Opened"); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if msg.deletes != 1 || len(msg.edits) != 1 {
		t.Fatalf("deletes=%d edits=%d, want delete first, then one edit", msg.deletes, len(msg.edits))
	}
	edit := msg.edits[0]
	if edit.Text != "Opened" {
		t.Errorf("edited text = %q, want %q", edit.Text, "Opened")
	}
	if len(edit.ReplyMarkup.InlineKeyboard) != 0 {
		t.Errorf("edit keeps buttons: %+v", edit.ReplyMarkup)
	}
}

func TestRetireReportsBothFailures(t *testing.T) {
	msg := &fakeMessage{deleteErr: errTooOld, editErr: errEditFail}
	err := Retire(nil, msg, "Opened")
	if !errors.Is(err, errTooOld) || !errors.Is(err, errEditFail) {
		t.Errorf("error %v should wrap both failures", err)
	}
}

// The callback query's message type must satisfy Message.
var _ Message = gotgbot.MaybeInaccessibleMessage(nil)

func TestSourceGone(t *testing.T) {
	gone := []error{
		&gotgbot.TelegramError{Code: 400, Description: "Bad Request: message to copy not found"},
		fmt.Errorf("unable to copyMessage: %w", &gotgbot.TelegramError{Code: 400, Description: "Bad Request: chat not found"}),
	}
	for _, err := range gone {
		if !SourceGone(err) {
			t.Errorf("SourceGone(%v) = false, want true", err)
		}
	}
	notGone := []error{
		nil,
		errors.New("message to copy not found"), // not a Telegram API error
		&gotgbot.TelegramError{Code: 429, Description: "Too Many Requests: retry after 5"},
		&gotgbot.TelegramError{Code: 403, Description: "Forbidden: bot was blocked by the user"},
	}
	for _, err := range notGone {
		if SourceGone(err) {
			t.Errorf("SourceGone(%v) = true, want false", err)
		}
	}
}

func TestMarkUnavailable(t *testing.T) {
	msg := &fakeMessage{}
	if err := MarkUnavailable(nil, msg, "Deleted"); err != nil {
		t.Fatalf("MarkUnavailable: %v", err)
	}
	if msg.deletes != 0 || len(msg.edits) != 1 {
		t.Fatalf("deletes=%d edits=%d, want only one edit (the note must stay visible)", msg.deletes, len(msg.edits))
	}
	if msg.edits[0].Text != "Deleted" || len(msg.edits[0].ReplyMarkup.InlineKeyboard) != 0 {
		t.Errorf("edit = %+v, want the text and no buttons", msg.edits[0])
	}
	failing := &fakeMessage{editErr: errEditFail}
	if err := MarkUnavailable(nil, failing, "Deleted"); !errors.Is(err, errEditFail) {
		t.Errorf("error %v should wrap the edit failure", err)
	}
}
