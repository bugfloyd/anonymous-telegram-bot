package notification

import (
	"errors"
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
