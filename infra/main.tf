locals {
  workspace_prefixes = {
    default     = "dev"
    development = "dev"
    production  = "prod"
  }
}

provider "aws" {
  region = var.aws_region
  profile = var.aws_profile[terraform.workspace]
}

# Lambda
resource "aws_lambda_function" "anonymous_bot" {
  function_name = "AnonymousBot"

  s3_bucket = "${local.workspace_prefixes[terraform.workspace]}.${var.lambda_bucket}"
  s3_key    = "lambda_function.zip"

  handler = "main"
  runtime = "provided.al2023"

  role = aws_iam_role.lambda_exec_role.arn

  source_code_hash = filebase64sha256(var.zip_bundle_path)

  environment {
    variables = {
      BOT_TOKEN = var.bot_token
    }
  }
}

resource "aws_iam_role" "lambda_exec_role" {
  name = "lambda_exec_role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = "lambda.amazonaws.com"
        }
      },
    ]
  })
}

resource "aws_iam_role_policy" "lambda_policy" {
  role = aws_iam_role.lambda_exec_role.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Action = [
          "logs:CreateLogGroup",
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ],
        Effect   = "Allow",
        Resource = "arn:aws:logs:*:*:*"
      },
    ]
  })
}

# API Gateway
resource "aws_apigatewayv2_api" "http_api" {
  name          = "BotHTTPAPI"
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "lambda_integration" {
  api_id           = aws_apigatewayv2_api.http_api.id
  integration_type = "AWS_PROXY"
  integration_uri  = aws_lambda_function.anonymous_bot.invoke_arn
}

resource "aws_apigatewayv2_route" "route" {
  api_id    = aws_apigatewayv2_api.http_api.id
  route_key = "POST /anonymous-bot"
  target    = "integrations/${aws_apigatewayv2_integration.lambda_integration.id}"
}

resource "aws_apigatewayv2_stage" "default_stage" {
  api_id      = aws_apigatewayv2_api.http_api.id
  name        = "$default"
  auto_deploy = true
}

output "webhook_url" {
  value = "${aws_apigatewayv2_stage.default_stage.invoke_url}anonymous-bot"
}

resource "aws_lambda_permission" "api_gw_lambda" {
  statement_id  = "AllowExecutionFromAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.anonymous_bot.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.http_api.execution_arn}/*/*/*"
}

resource "aws_dynamodb_table" "main" {
  name         = "AnonymousBot"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "UUID"

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

  global_secondary_index {
    name            = "UserID-GSI"
    hash_key        = "UserID"
    projection_type = "ALL"
  }

  global_secondary_index {
    name            = "Username-GSI"
    hash_key        = "Username"
    projection_type = "ALL"
  }

  lifecycle {
    prevent_destroy = false
  }
}

resource "aws_iam_policy" "lambda_dynamodb_policy" {
  name        = "AnonymousDynamoDBLambdaPolicy"
  description = "Policy to allow Lambda function to manage DynamoDB"

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query",
          "dynamodb:BatchGetItem",
          "dynamodb:BatchWriteItem",
          "dynamodb:DescribeTable",
        ],
        Effect   = "Allow",
        Resource = aws_dynamodb_table.main.arn
      },
      {
        Action = [
          "dynamodb:Query",
        ],
        Effect = "Allow",
        Resource = [
          "${aws_dynamodb_table.main.arn}/index/UserID-GSI",
          "${aws_dynamodb_table.main.arn}/index/Username-GSI"
        ]
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "rest_backend_lambda_dynamodb_policy_attachment" {
  role       = aws_iam_role.lambda_exec_role.name
  policy_arn = aws_iam_policy.lambda_dynamodb_policy.arn
}