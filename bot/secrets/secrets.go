package secrets

import (
	"encoding/json"
	"log"
	"os"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager"
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

func init() {
	secretName := "incognito-chat-bot-secrets"
	awsRegion := os.Getenv("AWS_REGION")

	token := os.Getenv("BOT_TOKEN")
	alphabet := os.Getenv("SQIDS_ALPHABET")

	if token == "" || alphabet == "" {
		// Create an AWS session
		sess, err := session.NewSession(&aws.Config{
			Region: aws.String(awsRegion),
		})
		if err != nil {
			log.Fatalf("Failed to create session: %v", err)
		}

		// Create a Secrets Manager client
		svc := secretsmanager.New(sess)

		// Retrieve the secret value
		input := &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(secretName),
		}

		result, err := svc.GetSecretValue(input)
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
}
