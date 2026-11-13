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
      timeout = 10
      # Measured on dev at 128 MB: most calls take ~20 ms waiting on DynamoDB,
      # but a batch of 20 send tokens (RSA signing, pure CPU) took ~4.5 s.
      # 256 MB doubles the CPU share; slow requests are logged with their
      # route and duration (internal/api) to size this from data.
      memory     = 256
      tables     = ["directory", "messages", "ephemeral"]
      parameters = ["bot-id", "session-key", "telegram-id-pepper", "issuer-key"]
      environment = {
        TABLE_DIRECTORY          = aws_dynamodb_table.table["directory"].name
        TABLE_MESSAGES           = aws_dynamodb_table.table["messages"].name
        TABLE_EPHEMERAL          = aws_dynamodb_table.table["ephemeral"].name
        PARAM_BOT_ID             = aws_ssm_parameter.parameter["bot-id"].name
        PARAM_SESSION_KEY        = aws_ssm_parameter.parameter["session-key"].name
        PARAM_TELEGRAM_ID_PEPPER = aws_ssm_parameter.parameter["telegram-id-pepper"].name
        PARAM_ISSUER_KEY         = aws_ssm_parameter.parameter["issuer-key"].name
        NOTIFY_FUNCTION          = "${local.prefix}-notify"
        MEDIA_BUCKET             = aws_s3_bucket.media.id
        WS_ENDPOINT              = local.ws_endpoint
        INVITE_ONLY              = tostring(var.invite_only)
        ADMIN_TELEGRAM_IDS       = var.admin_telegram_ids
      }
    }
    # Telegram webhook: opens the app, cleans up service messages. The deploy
    # invokes it once with a setup event to register the webhook.
    bot = {
      timeout    = 10
      tables     = []
      parameters = ["bot-token", "webhook-secret"]
      environment = {
        PARAM_BOT_TOKEN      = aws_ssm_parameter.parameter["bot-token"].name
        PARAM_WEBHOOK_SECRET = aws_ssm_parameter.parameter["webhook-secret"].name
        # The Mini App the bot's button opens: the client's origin.
        APP_URL = "${var.client_origins[0]}/"
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
    # Generic "New message" notices through the bot. api invokes it to seal
    # chat IDs, and without waiting after each delivery. Its quiet periods in
    # the ephemeral table get their own, narrower statement below.
    notify = {
      timeout    = 10
      tables     = ["directory"]
      parameters = ["bot-token", "chat-key"]
      environment = {
        TABLE_DIRECTORY = aws_dynamodb_table.table["directory"].name
        TABLE_EPHEMERAL = aws_dynamodb_table.table["ephemeral"].name
        PARAM_BOT_TOKEN = aws_ssm_parameter.parameter["bot-token"].name
        PARAM_CHAT_KEY  = aws_ssm_parameter.parameter["chat-key"].name
        APP_URL         = "${var.client_origins[0]}/"
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

  # Quiet periods only (backend/internal/ephemeral, "Q#" keys).
  dynamic "statement" {
    for_each = each.key == "notify" ? [1] : []
    content {
      sid       = "QuietPeriods"
      actions   = ["dynamodb:PutItem"]
      resources = [aws_dynamodb_table.table["ephemeral"].arn]
      condition {
        test     = "ForAllValues:StringLike"
        variable = "dynamodb:LeadingKeys"
        values   = ["Q#*"]
      }
    }
  }

  dynamic "statement" {
    for_each = each.key == "api" ? [1] : []
    content {
      sid       = "Media"
      actions   = ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"]
      resources = ["${aws_s3_bucket.media.arn}/m/*"]
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

# Each build is uploaded, signed by AWS Signer, and the signed copy is
# deployed. The code signing config (infra/init) makes Lambda refuse code
# that isn't signed by CI's profile. A new upload happens only when a build
# changes, and each upload gets a new signing job and a new signed copy.
resource "aws_s3_object" "build" {
  for_each = local.functions

  bucket      = var.lambda_builds_bucket
  key         = "builds/${each.key}.zip"
  source      = "${var.lambda_dir}/${each.key}.zip"
  source_hash = filesha256("${var.lambda_dir}/${each.key}.zip")
}

resource "aws_signer_signing_job" "build" {
  for_each = local.functions

  profile_name = var.lambda_signing_profile

  source {
    s3 {
      bucket  = aws_s3_object.build[each.key].bucket
      key     = aws_s3_object.build[each.key].key
      version = aws_s3_object.build[each.key].version_id
    }
  }

  destination {
    s3 {
      bucket = var.lambda_builds_bucket
      prefix = "signed/${each.key}/"
    }
  }
}

resource "aws_lambda_function" "fn" {
  for_each = local.functions

  function_name           = "${local.prefix}-${each.key}"
  description             = "Incognito Chat ${each.key} (${var.environment})"
  role                    = aws_iam_role.fn[each.key].arn
  runtime                 = "provided.al2023"
  handler                 = "bootstrap"
  architectures           = ["arm64"]
  memory_size             = lookup(each.value, "memory", 128)
  timeout                 = each.value.timeout
  s3_bucket               = aws_signer_signing_job.build[each.key].signed_object[0].s3[0].bucket
  s3_key                  = aws_signer_signing_job.build[each.key].signed_object[0].s3[0].key
  code_signing_config_arn = var.lambda_code_signing_config_arn

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

# Notices are sent at once or not at all: a retry would come after the quiet
# period started and send nothing, and a late notice is no use.
resource "aws_lambda_function_event_invoke_config" "notify" {
  function_name                = aws_lambda_function.fn["notify"].function_name
  maximum_retry_attempts       = 0
  maximum_event_age_in_seconds = 60
}

# Public HTTPS endpoints. The functions authenticate requests themselves:
# session tokens for api, the webhook secret header for bot.
resource "aws_lambda_function_url" "api" {
  function_name      = aws_lambda_function.fn["api"].function_name
  authorization_type = "NONE"

  cors {
    allow_origins = var.client_origins
    allow_methods = ["GET", "POST", "PUT", "DELETE"]
    allow_headers = ["authorization", "content-type", "x-device-signature"]
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
