package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/bugfloyd/telegram-incognito-bot/common"
)

func main() {
	lambda.Start(common.InitBot)
}
