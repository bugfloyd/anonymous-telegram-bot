variable "aws_region" {
  description = "The AWS region to create resources in"
  type        = string
}

variable "aws_profile" {
  description = "AWS profile to be used"
  type = map(string)
  default = {
    development = "dev"
    production  = "prod"
  }
}

variable "lambda_bucket" {
  description = "The AWS S3 bucket for storing lambda function codes"
  type        = string
}

variable "bot_token" {
  description = "Telegram bot token"
  type        = string
}

variable "zip_bundle_path" {
  description = "Local Lambda zip bundle path"
  type        = string
  default     = "../bot/lambda_function.zip"
}