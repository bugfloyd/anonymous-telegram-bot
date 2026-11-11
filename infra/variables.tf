variable "aws_region" {
  description = "AWS region for every resource"
  type        = string
}

variable "environment" {
  description = "Environment name; must match the bootstrap stack (infra/init), which only allows names with this prefix"
  type        = string

  validation {
    condition     = can(regex("^[a-z][a-z0-9]{1,15}$", var.environment))
    error_message = "environment must be 2-16 lowercase letters or digits, starting with a letter."
  }
}

variable "bot_token" {
  description = "Telegram bot token. Ephemeral: written to SSM, never stored in Terraform state or plan files."
  type        = string
  sensitive   = true
  ephemeral   = true
  nullable    = false

  validation {
    condition     = can(regex("^[0-9]+:[A-Za-z0-9_-]+$", var.bot_token))
    error_message = "bot_token doesn't look like a Telegram bot token (<digits>:<letters, digits, _ or ->)."
  }
}

variable "issuer_key" {
  description = "Send-token issuer's RSA-2048 private key (PKCS#8 PEM). Ephemeral: written to SSM, never stored in Terraform state or plan files. Its public key is published in web/issuer-keys.json."
  type        = string
  sensitive   = true
  ephemeral   = true
  nullable    = false

  validation {
    condition     = can(regex("^-----BEGIN PRIVATE KEY-----\\n[A-Za-z0-9+/=\\n]+-----END PRIVATE KEY-----\\n?$", var.issuer_key))
    error_message = "issuer_key must be a PKCS#8 PEM private key."
  }
}

variable "issuer_key_version" {
  description = "Increase to push a changed issuer key into SSM"
  type        = number
  default     = 1
}

variable "bot_token_version" {
  description = "Increase to push a changed bot token into SSM (write-only values can't be compared with what's stored)"
  type        = number
  default     = 1
}

variable "secrets_version" {
  description = "Increase to replace the generated secrets (session key, Telegram ID pepper, webhook secret). Replacing the pepper starts every account over."
  type        = number
  default     = 1
}

variable "client_origins" {
  description = "Origins allowed to call the HTTPS API from a browser (CORS), e.g. the GitHub Pages origin"
  type        = list(string)

  validation {
    condition     = length(var.client_origins) > 0 && alltrue([for o in var.client_origins : can(regex("^https://[a-z0-9.-]+$", o))])
    error_message = "client_origins must be a non-empty list of https:// origins without a path."
  }
}

variable "invite_only" {
  description = "While true, only accounts that joined through an invitation, and admins, can own links"
  type        = bool
  default     = true
}

variable "admin_telegram_ids" {
  description = "Comma-separated Telegram user IDs of admins. The api function keeps only their keyed hashes"
  type        = string
  default     = ""
  sensitive   = true

  validation {
    condition     = can(regex("^[0-9, ]*$", var.admin_telegram_ids))
    error_message = "admin_telegram_ids must be comma-separated numeric Telegram user IDs."
  }
}

variable "lambda_dir" {
  description = "Directory with the Lambda bundles built by scripts/build-lambdas.sh"
  type        = string
  default     = "../backend/dist"
}

variable "log_retention_days" {
  description = "Retention for function logs"
  type        = number
  default     = 7
}

variable "deletion_protection" {
  description = "Protect the DynamoDB tables from deletion (keep on in production)"
  type        = bool
  default     = true
}

variable "table_max_read_units" {
  description = "Per-table cap on on-demand read request units per second, a cost guard"
  type        = number
  default     = 200
}

variable "table_max_write_units" {
  description = "Per-table cap on on-demand write request units per second, a cost guard"
  type        = number
  default     = 100
}

variable "ws_throttle_rate_limit" {
  description = "Steady-state requests per second allowed on the WebSocket API"
  type        = number
  default     = 20
}

variable "ws_throttle_burst_limit" {
  description = "Burst of requests allowed on the WebSocket API"
  type        = number
  default     = 40
}
