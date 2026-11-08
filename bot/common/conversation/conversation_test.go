package conversation

import (
	"testing"
	"time"
)

func TestCanFollowUp(t *testing.T) {
	last := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		lastSentAt int64
		sentAt     time.Time
		want       bool
	}{
		{"no open conversation", 0, last, false},
		{"right after", last.Unix(), last.Add(5 * time.Second), true},
		{"within the window", last.Unix(), last.Add(14 * time.Minute), true},
		{"exactly at the window", last.Unix(), last.Add(FollowUpWindow), true},
		{"just after the window", last.Unix(), last.Add(FollowUpWindow + time.Second), false},
		{"next day", last.Unix(), last.Add(24 * time.Hour), false},
		{"timestamps slightly out of order", last.Unix(), last.Add(-2 * time.Second), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanFollowUp(tt.lastSentAt, tt.sentAt); got != tt.want {
				t.Errorf("CanFollowUp() = %v, want %v", got, tt.want)
			}
		})
	}
}
