package main

import (
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/bugfloyd/anonymous-telegram-bot/anonymous"
)

func main() {
	lambda.Start(anonymous.InitBot)
}
