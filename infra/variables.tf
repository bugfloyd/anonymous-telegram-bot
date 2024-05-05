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

variable "sqids_alphabet" {
  description = "sqids alphabet"
  type        = string
}

variable "default_language" {
  description = "Default language for bot"
  type        = string
}

variable "zip_bundle_path" {
  description = "Local Lambda zip bundle path"
  type        = string
  default     = "../bot/lambda_function.zip"
}