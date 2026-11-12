# Secrets and settings in SSM Parameter Store (SecureString, standard tier:
# free). Values are generated as ephemeral values or passed in as ephemeral
# variables and written with write-only arguments, so they never appear in
# Terraform state or plan files. Each function role can read only the
# parameters it needs (functions.tf).

# HMAC key for session tokens (api issues them, ws checks them).
ephemeral "random_password" "session_key" {
  length  = 64
  special = false
}

# Key for the keyed hash of Telegram user IDs, so the directory can enforce one
# account per Telegram user without storing the IDs themselves.
ephemeral "random_password" "telegram_id_pepper" {
  length  = 64
  special = false
}

# Telegram sends it with every webhook update; independent of the bot token.
ephemeral "random_password" "webhook_secret" {
  length  = 64
  special = false
}

# Seals the Telegram chat IDs notices go to; only notify can read it. A new
# key leaves stored chat IDs unreadable: notices stop until each app sets
# them up again, which it does when the account says they're off.
ephemeral "random_password" "chat_key" {
  length  = 64
  special = false
}

locals {
  # Versions decide when a write-only value is written again. Kept separate
  # from the values because values are ephemeral and can't drive for_each.
  parameter_versions = {
    "bot-token"          = var.bot_token_version
    "bot-id"             = var.bot_token_version
    "session-key"        = var.secrets_version
    "telegram-id-pepper" = var.secrets_version
    "webhook-secret"     = var.secrets_version
    "chat-key"           = var.secrets_version
    "issuer-key"         = var.issuer_key_version
  }

  # The bot ID is the token's numeric prefix. Writing it here means nobody
  # has to read the token to configure it, and the api function can check
  # Telegram's signatures without having access to the token.
  parameter_values = {
    "bot-token"          = var.bot_token
    "bot-id"             = split(":", var.bot_token)[0]
    "session-key"        = ephemeral.random_password.session_key.result
    "telegram-id-pepper" = ephemeral.random_password.telegram_id_pepper.result
    "webhook-secret"     = ephemeral.random_password.webhook_secret.result
    "chat-key"           = ephemeral.random_password.chat_key.result
    "issuer-key"         = var.issuer_key
  }
}

resource "aws_ssm_parameter" "parameter" {
  for_each = local.parameter_versions

  name             = "${local.parameter_path}/${each.key}"
  type             = "SecureString"
  tier             = "Standard"
  value_wo         = local.parameter_values[each.key]
  value_wo_version = each.value
}
