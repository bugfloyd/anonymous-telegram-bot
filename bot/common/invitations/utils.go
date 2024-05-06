package invitations

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

// generateRandomString generates a random string of a specified length from a predefined charset.
func generateRandomString(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

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
func generateUniqueInvitationCode(repo *Repository) (string, error) {
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
