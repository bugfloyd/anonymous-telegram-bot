// Command bot receives Telegram webhook updates through a Lambda function URL.
// It only opens the app and cleans up service messages; it never sees message
// content. Until that is built, every request gets a 404.
package main

import (
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

func main() {
	router := httpapi.NewRouter()
	lambda.Start(router.Serve)
}
