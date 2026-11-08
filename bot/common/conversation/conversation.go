// Package conversation decides when a user's next message continues their
// current conversation.
//
// After a user's message is delivered, their following messages go to the
// same person ("follow-ups") until either:
//   - the bot shows them something else: a message from someone other than
//     that person, or a response to a command or button (enforced where those
//     happen, see UserRepository.InterruptFollowUps), or
//   - FollowUpWindow passes without them sending anything (checked here).
package conversation

import "time"

// FollowUpWindow is how long after a user's last delivered message their next
// message still goes to the same person.
const FollowUpWindow = 15 * time.Minute

// CanFollowUp reports whether a message sent at sentAt continues the
// conversation whose previous message was delivered at lastSentAt (unix
// seconds; 0 means no open conversation).
func CanFollowUp(lastSentAt int64, sentAt time.Time) bool {
	if lastSentAt == 0 {
		return false
	}
	return sentAt.Sub(time.Unix(lastSentAt, 0)) <= FollowUpWindow
}
