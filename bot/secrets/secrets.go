package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/bugfloyd/telegram-incognito-bot/common/webhook"
)

// SecretStruct represents the structure of the JSON-encoded secret string
type SecretStruct struct {
	BotToken    string `json:"bot_token"`
	Alphabet    string `json:"alphabet"`
	IdentityKey string `json:"identity_key"`
}

var BotToken string
var SqidsAlphabet string

// WebhookSecret is the token Telegram must send with every webhook update.
// It is derived from BotToken, see the webhook package.
var WebhookSecret string

// IdentityKey protects who is who: the data of buttons that refer to another
// user is sealed with it, and people's labels are derived from it (see the
// buttons and labels packages). It is 32 random bytes, generated once by
// Terraform and never derived from the bot token, so rotating the token keeps
// buttons working and labels unchanged.
var IdentityKey []byte

// minIdentityKeyLength must match buttons.MinKeyLength.
const minIdentityKeyLength = 32

func init() {
	secretName := "incognito-chat-bot-secrets" // must match infra/main.tf

	token := os.Getenv("BOT_TOKEN")
	alphabet := os.Getenv("SQIDS_ALPHABET")
	identityKey := os.Getenv("IDENTITY_KEY")

	if token == "" || alphabet == "" || identityKey == "" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Region and credentials come from the environment (AWS_REGION and the
		// Lambda execution role).
		cfg, err := config.LoadDefaultConfig(ctx)
		if err != nil {
			log.Fatalf("Failed to load AWS config: %v", err)
		}

		result, err := secretsmanager.NewFromConfig(cfg).GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(secretName),
		})
		if err != nil {
			log.Fatalf("Failed to get secret: %v", err)
		}

		// Parse the JSON-encoded secret string into the struct
		var secret SecretStruct
		if err := json.Unmarshal([]byte(*result.SecretString), &secret); err != nil {
			log.Fatalf("Failed to unmarshal secret: %v", err)
		}

		// Assign the secret fields to global variables
		BotToken = secret.BotToken
		SqidsAlphabet = secret.Alphabet
		identityKey = secret.IdentityKey
	} else {
		BotToken = token
		SqidsAlphabet = alphabet
	}

	if BotToken == "" {
		log.Fatal("Bot token is empty")
	}
	WebhookSecret = webhook.DeriveSecret(BotToken)

	key, err := base64.StdEncoding.DecodeString(identityKey)
	if err != nil || len(key) < minIdentityKeyLength {
		log.Fatalf("Identity key must be at least %d bytes, base64-encoded", minIdentityKeyLength)
	}
	IdentityKey = key
}
