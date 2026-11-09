package conversation

import "testing"

func TestRoleOpposite(t *testing.T) {
	if Anonymous.Opposite() != Known || Known.Opposite() != Anonymous {
		t.Error("Anonymous and Known must be each other's opposite")
	}
	if Role("").Opposite() != "" || Role("other").Opposite() != "" {
		t.Error("an invalid role has no opposite")
	}
}

func TestRoleValid(t *testing.T) {
	for role, want := range map[Role]bool{Anonymous: true, Known: true, "": false, "anonymous": false} {
		if got := role.Valid(); got != want {
			t.Errorf("Role(%q).Valid() = %v, want %v", role, got, want)
		}
	}
}
