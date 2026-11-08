variable "aws_region" {
  description = "The AWS region to create resources in"
  type        = string
}

variable "bot_token" {
  description = "Telegram bot token. Ephemeral: written to Secrets Manager but never stored in Terraform state or plan files."
  type        = string
  sensitive   = true
  ephemeral   = true
  nullable    = false

  validation {
    condition     = can(regex("^[0-9]+:[A-Za-z0-9_-]+$", var.bot_token))
    error_message = "bot_token doesn't look like a Telegram bot token (<digits>:<letters, digits, _ or ->)."
  }
}

variable "bot_secret_version" {
  description = "Increase this to push a changed bot token into Secrets Manager (write-only values can't be compared with what's stored)"
  type        = number
  default     = 1
}

variable "default_language" {
  description = "Default language for the bot (en_US or fa_IR). Empty means detect from the user's Telegram client."
  type        = string
  default     = ""

  validation {
    condition     = contains(["", "en_US", "fa_IR"], var.default_language)
    error_message = "default_language must be empty, en_US or fa_IR."
  }
}

variable "lambda_zip_path" {
  description = "Path to the Lambda zip bundle (built by CI or locally, see README)"
  type        = string
  default     = "../bot/lambda_function.zip"
}

variable "log_retention_days" {
  description = "Retention for Lambda and API access logs"
  type        = number
  default     = 30
}

variable "api_throttle_rate_limit" {
  description = "Steady-state requests per second allowed on the webhook endpoint"
  type        = number
  default     = 10
}

variable "api_throttle_burst_limit" {
  description = "Burst of requests allowed on the webhook endpoint"
  type        = number
  default     = 20
}
