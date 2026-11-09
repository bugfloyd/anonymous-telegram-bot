package conversation

// Role is how a user takes part in a conversation.
//
// Every conversation starts when someone opens another person's link: the
// person who opened it is Anonymous and the link's owner is Known. The same
// two people can also have a second conversation the other way around (each
// opened the other's link), and the two must never be linkable: everything
// that identifies the other person (button data, labels) is derived from
// their role as well as from who they are.
type Role string

const (
	// Anonymous: opened the other person's link.
	Anonymous Role = "ANONYMOUS"
	// Known: owns the link the other person opened.
	Known Role = "KNOWN"
)

// Valid reports whether r is one of the two roles.
func (r Role) Valid() bool {
	return r == Anonymous || r == Known
}

// Opposite is the other person's role in the same conversation.
func (r Role) Opposite() Role {
	switch r {
	case Anonymous:
		return Known
	case Known:
		return Anonymous
	default:
		return ""
	}
}
