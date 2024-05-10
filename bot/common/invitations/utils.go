package invitations

import (
	"crypto/rand"
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/PaulSonOfLars/gotgbot/v2/ext"
	"math/big"
	"strings"
)

// generateRandomString generates a random string of a specified length from a predefined charset.
func generateRandomString(length int) (string, error) {
	const charset = "abcdefghijkmnpqrstuvwxyz123456789"

	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}
	return string(result), nil
}

// getRandomLength returns a random integer between min and max inclusive.
func getRandomLength(min, max int) (int, error) {
	if min > max {
		return 0, fmt.Errorf("min should not be greater than max")
	}
	num, err := rand.Int(rand.Reader, big.NewInt(int64(max-min+1)))
	if err != nil {
		return 0, err
	}
	return int(num.Int64()) + min, nil
}

// generateUniqueInvitationCode generates a unique invitation code by ensuring it's not already in DynamoDB.
func generateUniqueInvitationCode() (string, error) {
	// create invitations repo
	repo, err := NewRepository()
	if err != nil {
		return "", fmt.Errorf("failed to init invitations db repo: %w", err)
	}

	for {
		// Get random lengths for both sections
		section1Len, err := getRandomLength(3, 5)
		if err != nil {
			return "", err
		}
		section2Len, err := getRandomLength(3, 5)
		if err != nil {
			return "", err
		}

		// Generate two sections of random alphanumeric strings
		section1, err := generateRandomString(section1Len)
		if err != nil {
			return "", err
		}
		section2, err := generateRandomString(section2Len)
		if err != nil {
			return "", err
		}

		// Concatenate the sections with a hyphen
		code := fmt.Sprintf("%s-%s", section1, section2)

		// Check if the generated code already exists
		existingInvitation, err := repo.readInvitation("whisper-" + code)
		if err != nil && !strings.Contains(err.Error(), "dynamo: no item found") {
			return "", err
		}

		// If no existing invitation is found, the code is unique
		if existingInvitation == nil || strings.Contains(err.Error(), "dynamo: no item found") {
			return code, nil
		}
		// Otherwise, keep generating until a unique code is found
	}
}

func isInvited(repo *Repository, userUUID string) bool {
	user, err := repo.readUser(userUUID)
	if user == nil || err != nil {
		return false
	}
	return true
}

func CheckUserInvitation(userUUID string, b *gotgbot.Bot, ctx *ext.Context) bool {
	// create invitations repo
	repo, err := NewRepository()
	if err != nil {
		fmt.Sprintln("failed to init invitations db repo: %w", err)
		return false
	}

	isValid := isInvited(repo, userUUID)
	if !isValid {
		_, err = ctx.EffectiveMessage.Reply(b, "The bot is not public yet! :(\nIf you have an invitation code, run /register command to activate your account.", nil)
		if err != nil {
			fmt.Sprintln("failed to reply: %w", err)
		}
		return false
	}

	return true
}
