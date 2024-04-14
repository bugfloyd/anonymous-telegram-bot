package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/PaulSonOfLars/gotgbot/v2"
	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"log"
	"net/http"
	"os"
)

type Response events.APIGatewayProxyResponse
type Request events.APIGatewayProxyRequest

// Update represents the incoming Telegram update.
type Update struct {
	Message *gotgbot.Message `json:"message"`
}

// Handler is our lambda handler invoked by the `lambda.Start` function call
func Handler(request Request) (Response, error) {
	// Get token from the environment variable.
	token := os.Getenv("TOKEN")
	if token == "" {
		return Response{StatusCode: 500}, errors.New("TOKEN environment variable is empty")
	}

	// Unmarshal the update from Telegram
	var update Update
	err := json.Unmarshal([]byte(request.Body), &update)
	if err != nil {
		log.Printf("Error unmarshaling update: %v", err)
		return Response{StatusCode: 400}, fmt.Errorf("error unmarshaling update: %w", err)
	}

	// Create bot from environment value.
	b, err := gotgbot.NewBot(token, &gotgbot.BotOpts{
		BotClient: &gotgbot.BaseBotClient{
			Client: http.Client{},
		},
	})
	if err != nil {
		return Response{StatusCode: 500}, fmt.Errorf("failed to create new bot: %w", err)
	}

	if update.Message != nil {
		// Echo the received message back to the user
		_, err := update.Message.Reply(b, update.Message.Text, nil)
		if err != nil {
			log.Printf("Error sending reply: %v", err)
			return Response{StatusCode: 500}, fmt.Errorf("failed to send reply: %w", err)
		}
	}

	// Return a successful response with the message
	return Response{
		StatusCode: 200,
		Body:       "success",
	}, nil
}

func main() {
	// Start the lambda handler
	lambda.Start(Handler)
}
