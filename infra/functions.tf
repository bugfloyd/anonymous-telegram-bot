# Four functions, one per trust boundary. Each has its own role, limited to
# its own tables, parameters and log group, inside the permissions boundary
# from infra/init. Only bot and notify can read the bot token; only api can
# push to sockets and invoke notify.
locals {
  ws_endpoint = "https://${aws_apigatewayv2_api.ws.id}.execute-api.${var.aws_region}.amazonaws.com/${aws_apigatewayv2_stage.ws.name}"
  notify_arn  = "arn:${local.partition}:lambda:${var.aws_region}:${local.account_id}:function:${local.prefix}-notify"

  functions = {
    # HTTPS API for the client.
    api = {
      timeout    = 10
      tables     = ["directory", "messages", "ephemeral"]
      parameters = ["bot-id", "session-key", "telegram-id-pepper"]
      environment = {
        TABLE_DIRECTORY          = aws_dynamodb_table.table["directory"].name
        TABLE_MESSAGES           = aws_dynamodb_table.table["messages"].name
        TABLE_EPHEMERAL          = aws_dynamodb_table.table["ephemeral"].name
        PARAM_BOT_ID             = aws_ssm_parameter.parameter["bot-id"].name
        PARAM_SESSION_KEY        = aws_ssm_parameter.parameter["session-key"].name
        PARAM_TELEGRAM_ID_PEPPER = aws_ssm_parameter.parameter["telegram-id-pepper"].name
        NOTIFY_FUNCTION          = "${local.prefix}-notify"
        WS_ENDPOINT              = local.ws_endpoint
      }
    }
    # Telegram webhook: opens the app, cleans up service messages.
    bot = {
      timeout    = 10
      tables     = []
      parameters = ["bot-token", "webhook-secret"]
      environment = {
        PARAM_BOT_TOKEN      = aws_ssm_parameter.parameter["bot-token"].name
        PARAM_WEBHOOK_SECRET = aws_ssm_parameter.parameter["webhook-secret"].name
      }
    }
    # WebSocket $connect / $disconnect.
    ws = {
      timeout    = 5
      tables     = ["ephemeral"]
      parameters = ["session-key"]
      environment = {
        TABLE_EPHEMERAL   = aws_dynamodb_table.table["ephemeral"].name
        PARAM_SESSION_KEY = aws_ssm_parameter.parameter["session-key"].name
      }
    }
    # Generic "New message" notices through the bot.
    notify = {
      timeout    = 10
      tables     = ["directory"]
      parameters = ["bot-token"]
      environment = {
        TABLE_DIRECTORY = aws_dynamodb_table.table["directory"].name
        PARAM_BOT_TOKEN = aws_ssm_parameter.parameter["bot-token"].name
      }
    }
  }
}

resource "aws_cloudwatch_log_group" "fn" {
  for_each = local.functions

  name              = "/aws/lambda/${local.prefix}-${each.key}"
  retention_in_days = var.log_retention_days
}

data "aws_iam_policy_document" "lambda_trust" {
  statement {
    actions = ["sts:AssumeRole"]

    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "fn" {
  for_each = local.functions

  name                 = "${local.prefix}-fn-${each.key}"
  description          = "Execution role of the ${each.key} function"
  assume_role_policy   = data.aws_iam_policy_document.lambda_trust.json
  permissions_boundary = local.lambda_boundary_arn
}

data "aws_iam_policy_document" "fn" {
  for_each = local.functions

  statement {
    sid       = "Logs"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${aws_cloudwatch_log_group.fn[each.key].arn}:*"]
  }

  dynamic "statement" {
    for_each = length(each.value.tables) > 0 ? [1] : []
    content {
      sid = "Tables"
      actions = [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:UpdateItem",
        "dynamodb:DeleteItem",
        "dynamodb:Query",
        "dynamodb:BatchGetItem",
        "dynamodb:BatchWriteItem",
        "dynamodb:ConditionCheckItem",
      ]
      resources = [for t in each.value.tables : aws_dynamodb_table.table[t].arn]
    }
  }

  statement {
    sid       = "Parameters"
    actions   = ["ssm:GetParameter", "ssm:GetParameters"]
    resources = [for p in each.value.parameters : aws_ssm_parameter.parameter[p].arn]
  }

  dynamic "statement" {
    for_each = each.key == "api" ? [1] : []
    content {
      sid     = "PushToSockets"
      actions = ["execute-api:ManageConnections"]
      resources = [
        "${aws_apigatewayv2_api.ws.execution_arn}/${aws_apigatewayv2_stage.ws.name}/POST/@connections/*",
        "${aws_apigatewayv2_api.ws.execution_arn}/${aws_apigatewayv2_stage.ws.name}/DELETE/@connections/*",
      ]
    }
  }

  dynamic "statement" {
    for_each = each.key == "api" ? [1] : []
    content {
      sid       = "InvokeNotify"
      actions   = ["lambda:InvokeFunction"]
      resources = [local.notify_arn]
    }
  }
}

resource "aws_iam_role_policy" "fn" {
  for_each = local.functions

  name   = "runtime"
  role   = aws_iam_role.fn[each.key].id
  policy = data.aws_iam_policy_document.fn[each.key].json
}

resource "aws_lambda_function" "fn" {
  for_each = local.functions

  function_name    = "${local.prefix}-${each.key}"
  description      = "Incognito Chat ${each.key} (${var.environment})"
  role             = aws_iam_role.fn[each.key].arn
  runtime          = "provided.al2023"
  handler          = "bootstrap"
  architectures    = ["arm64"]
  memory_size      = 128
  timeout          = each.value.timeout
  filename         = "${var.lambda_dir}/${each.key}.zip"
  source_code_hash = filebase64sha256("${var.lambda_dir}/${each.key}.zip")

  # Functions read their parameters once, at cold start. PARAMS_VERSION
  # changes whenever one of this function's parameters is rewritten, which
  # updates the function and so replaces its warm instances.
  environment {
    variables = merge(
      {
        ENVIRONMENT    = var.environment
        PARAMS_VERSION = join(",", [for p in each.value.parameters : "${p}=${local.parameter_versions[p]}"])
      },
      each.value.environment,
    )
  }

  # JSON at warning level; the platform's START/END/REPORT lines are dropped
  # too. The code logs only allow-listed fields (backend/internal/logx).
  logging_config {
    log_format            = "JSON"
    application_log_level = "WARN"
    system_log_level      = "WARN"
    log_group             = aws_cloudwatch_log_group.fn[each.key].name
  }

  # New instances must read the new parameter values.
  depends_on = [aws_iam_role_policy.fn, aws_ssm_parameter.parameter]
}

# Public HTTPS endpoints. The functions authenticate requests themselves:
# session tokens for api, the webhook secret header for bot.
resource "aws_lambda_function_url" "api" {
  function_name      = aws_lambda_function.fn["api"].function_name
  authorization_type = "NONE"

  cors {
    allow_origins = var.client_origins
    allow_methods = ["GET", "POST", "PUT", "DELETE"]
    allow_headers = ["authorization", "content-type"]
    max_age       = 3600
  }
}

resource "aws_lambda_function_url" "bot" {
  function_name      = aws_lambda_function.fn["bot"].function_name
  authorization_type = "NONE"
}

# A public function URL needs both permissions: one for the URL, and one for
# the invocation behind it (limited to calls through the URL).
locals {
  public_functions = toset(["api", "bot"])
}

resource "aws_lambda_permission" "url" {
  for_each = local.public_functions

  statement_id           = "FunctionUrlPublic"
  action                 = "lambda:InvokeFunctionUrl"
  function_name          = aws_lambda_function.fn[each.key].function_name
  principal              = "*"
  function_url_auth_type = "NONE"
}

resource "aws_lambda_permission" "url_invoke" {
  for_each = local.public_functions

  statement_id             = "FunctionUrlInvoke"
  action                   = "lambda:InvokeFunction"
  function_name            = aws_lambda_function.fn[each.key].function_name
  principal                = "*"
  invoked_via_function_url = true
}
