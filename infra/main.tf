provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project   = "telegram-incognito-bot"
      Stack     = "app"
      ManagedBy = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  # These names are referenced by the bootstrap stack's deploy-role policy
  # (init/main.tf) and some by the bot code. Keep them in sync.
  function_name    = "IncognitoChatBot"
  table_name       = "IncognitoChatBot"           # bot/common/users/user_repo.go
  secret_name      = "incognito-chat-bot-secrets" # bot/secrets/secrets.go
  lambda_role_name = "incognito-chat-bot-lambda"
  api_name         = "incognito-chat-bot"
  webhook_path     = "/webhook"

  # Created by the bootstrap stack. The deploy role may only create the
  # Lambda role with this boundary attached.
  lambda_boundary_arn = "arn:${data.aws_partition.current.partition}:iam::${data.aws_caller_identity.current.account_id}:policy/incognito-chat-bot-lambda-boundary"
}

# ---------------------------------------------------------------------------
# Secrets
# ---------------------------------------------------------------------------

# Alphabet that sqids uses to encode user link keys. Generated once and never
# changed: changing it would break every link already shared.
resource "random_shuffle" "sqids_alphabet" {
  input = split("", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
}

resource "aws_secretsmanager_secret" "bot" {
  name                    = local.secret_name
  description             = "Telegram bot token and sqids alphabet"
  recovery_window_in_days = 7
}

resource "aws_secretsmanager_secret_version" "bot" {
  secret_id = aws_secretsmanager_secret.bot.id

  # Write-only: sent to AWS, never persisted in state or plan.
  secret_string_wo = jsonencode({
    bot_token = var.bot_token
    alphabet  = join("", random_shuffle.sqids_alphabet.result)
  })
  secret_string_wo_version = var.bot_secret_version
}

# ---------------------------------------------------------------------------
# DynamoDB
# ---------------------------------------------------------------------------

resource "aws_dynamodb_table" "main" {
  name                        = local.table_name
  billing_mode                = "PAY_PER_REQUEST"
  hash_key                    = "UUID"
  deletion_protection_enabled = true

  point_in_time_recovery {
    enabled = true
  }

  attribute {
    name = "UUID"
    type = "S"
  }

  attribute {
    name = "UserID"
    type = "N"
  }

  attribute {
    name = "Username"
    type = "S"
  }

  attribute {
    name = "LinkKey"
    type = "N"
  }

  attribute {
    name = "CreatedAt"
    type = "N"
  }

  global_secondary_index {
    name            = "UserID-GSI"
    projection_type = "ALL"

    key_schema {
      attribute_name = "UserID"
      key_type       = "HASH"
    }
  }

  global_secondary_index {
    name            = "Username-GSI"
    projection_type = "ALL"

    key_schema {
      attribute_name = "Username"
      key_type       = "HASH"
    }
  }

  global_secondary_index {
    name            = "LinkKey-GSI"
    projection_type = "ALL"

    key_schema {
      attribute_name = "LinkKey"
      key_type       = "HASH"
    }

    key_schema {
      attribute_name = "CreatedAt"
      key_type       = "RANGE"
    }
  }
}

# ---------------------------------------------------------------------------
# Lambda
# ---------------------------------------------------------------------------

resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${local.function_name}"
  retention_in_days = var.log_retention_days
}

data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "lambda" {
  name                 = local.lambda_role_name
  assume_role_policy   = data.aws_iam_policy_document.lambda_assume.json
  permissions_boundary = local.lambda_boundary_arn
}

data "aws_iam_policy_document" "lambda" {
  statement {
    sid       = "Logs"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.lambda.arn}:*"]
  }

  statement {
    sid       = "DynamoDB"
    actions   = ["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:Query"]
    resources = [aws_dynamodb_table.main.arn, "${aws_dynamodb_table.main.arn}/index/*"]
  }

  statement {
    sid       = "BotSecrets"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.bot.arn]
  }
}

resource "aws_iam_role_policy" "lambda" {
  name   = "runtime"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda.json
}

resource "aws_lambda_function" "bot" {
  function_name    = local.function_name
  description      = "Incognito chat bot Telegram webhook handler"
  role             = aws_iam_role.lambda.arn
  filename         = var.lambda_zip_path
  source_code_hash = filebase64sha256(var.lambda_zip_path)
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  memory_size      = 128
  timeout          = 10

  environment {
    variables = {
      DEFAULT_LANGUAGE = var.default_language
      # The bot reads its secret once per execution environment. Changing this
      # value replaces those environments, so a rotated token takes effect.
      BOT_SECRET_VERSION = tostring(var.bot_secret_version)
    }
  }

  depends_on = [
    aws_cloudwatch_log_group.lambda,
    aws_iam_role_policy.lambda,
    aws_secretsmanager_secret_version.bot,
  ]
}

# ---------------------------------------------------------------------------
# API Gateway (HTTP API) – Telegram webhook endpoint
# ---------------------------------------------------------------------------

resource "aws_apigatewayv2_api" "bot" {
  name          = local.api_name
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "lambda" {
  api_id                 = aws_apigatewayv2_api.bot.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.bot.invoke_arn
  payload_format_version = "1.0" # the bot uses events.APIGatewayProxyRequest
}

resource "aws_apigatewayv2_route" "webhook" {
  api_id    = aws_apigatewayv2_api.bot.id
  route_key = "POST ${local.webhook_path}"
  target    = "integrations/${aws_apigatewayv2_integration.lambda.id}"
}

resource "aws_cloudwatch_log_group" "api_access" {
  name              = "/aws/apigateway/${local.api_name}"
  retention_in_days = var.log_retention_days
}

resource "aws_apigatewayv2_stage" "default" {
  api_id      = aws_apigatewayv2_api.bot.id
  name        = "$default"
  auto_deploy = true

  # Limits how much traffic anyone hitting the public endpoint can cause.
  default_route_settings {
    throttling_rate_limit  = var.api_throttle_rate_limit
    throttling_burst_limit = var.api_throttle_burst_limit
  }

  access_log_settings {
    destination_arn = aws_cloudwatch_log_group.api_access.arn
    format = jsonencode({
      requestId         = "$context.requestId"
      requestTime       = "$context.requestTime"
      sourceIp          = "$context.identity.sourceIp"
      httpMethod        = "$context.httpMethod"
      routeKey          = "$context.routeKey"
      status            = "$context.status"
      responseLength    = "$context.responseLength"
      responseLatency   = "$context.responseLatency"
      integrationStatus = "$context.integrationStatus"
      integrationError  = "$context.integrationErrorMessage"
    })
  }
}

resource "aws_lambda_permission" "api_gateway" {
  statement_id  = "AllowWebhookRouteInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.bot.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.bot.execution_arn}/*/POST${local.webhook_path}"
}
