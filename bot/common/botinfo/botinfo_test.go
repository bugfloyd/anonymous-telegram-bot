package botinfo

import (
	"testing"

	"github.com/PaulSonOfLars/gotgbot/v2"
)

func TestText(t *testing.T) {
	tests := []struct {
		name string
		bot  gotgbot.User
		want string
	}{
		{
			name: "first name and username",
			bot:  gotgbot.User{FirstName: "Example Bot", Username: "example_bot"},
			want: "Example Bot (@example_bot)\n\nSource code:\n" + SourceCodeURL,
		},
		{
			name: "with last name",
			bot:  gotgbot.User{FirstName: "Example", LastName: "Bot", Username: "example_bot"},
			want: "Example Bot (@example_bot)\n\nSource code:\n" + SourceCodeURL,
		},
		{
			name: "without username",
			bot:  gotgbot.User{FirstName: "Example Bot"},
			want: "Example Bot\n\nSource code:\n" + SourceCodeURL,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Text(tt.bot); got != tt.want {
				t.Errorf("Text() = %q, want %q", got, tt.want)
			}
		})
	}
}
