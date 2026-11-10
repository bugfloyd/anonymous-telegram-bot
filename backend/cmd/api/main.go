// Command api serves the client's HTTPS API through a Lambda function URL.
// Routes are added as v2 is built; until then every request gets a 404.
package main

import (
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/incognitochat/incognitochat/backend/internal/httpapi"
)

func main() {
	router := httpapi.NewRouter()
	lambda.Start(router.Serve)
}
