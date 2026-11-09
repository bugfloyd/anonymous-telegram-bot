package secrets

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/bugfloyd/telegram-incognito-bot/common/buttons"
	"github.com/bugfloyd/telegram-incognito-bot/common/webhook"
)

// SecretStruct represents the structure of the JSON-encoded secret string
type SecretStruct struct {
	BotToken string `json:"bot_token"`
	Alphabet string `json:"alphabet"`
}

var BotToken string
var SqidsAlphabet string

// WebhookSecret is the token Telegram must send with every webhook update.
// It is derived from BotToken, see the webhook package.
var WebhookSecret string

// ButtonKey signs the data of buttons that refer to another user, see the
// buttons package. It is derived from BotToken.
var ButtonKey []byte

func init() {
	secretName := "incognito-chat-bot-secrets" // must match infra/main.tf

	token := os.Getenv("BOT_TOKEN")
	alphabet := os.Getenv("SQIDS_ALPHABET")

	if token == "" || alphabet == "" {
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
	} else {
		BotToken = token
		SqidsAlphabet = alphabet
	}

	if BotToken == "" {
		log.Fatal("Bot token is empty")
	}
	WebhookSecret = webhook.DeriveSecret(BotToken)
	ButtonKey = buttons.DeriveKey(BotToken)
}
