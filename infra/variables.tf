variable "aws_region" {
  description = "The AWS region to create resources in"
  type        = string
}

variable "lambda_bucket" {
  description = "The AWS S3 bucket for storing lambda function codes"
  type        = string
}

variable "bot_token" {
  description = "Telegram bot token"
  type        = string
}