provider "aws" {
  region = var.aws_region
}

# Lambda
resource "aws_lambda_function" "anonymous_bot" {
  function_name = "AnonymousBot"

  s3_bucket = var.lambda_bucket
  s3_key    = "lambda_function.zip"

  handler = "main"
  runtime = "provided.al2023"

  role = aws_iam_role.lambda_exec_role.arn

  source_code_hash = filebase64sha256("../bot/lambda_function.zip")

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
        Effect = "Allow",
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
  api_id        = aws_apigatewayv2_api.http_api.id
  name          = "$default"
  auto_deploy   = true
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