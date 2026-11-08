# Bootstrap stack: everything CI/CD needs before it can deploy the app stack
# (../). It's applied by an account admin via scripts/bootstrap.sh, never by CI,
# so the pipeline can't widen its own permissions.

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project   = "telegram-incognito-bot"
      Stack     = "bootstrap"
      ManagedBy = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = var.aws_region

  # Names of the resources managed by the app stack (../main.tf). The deploy
  # role may only touch these, so keep both stacks in sync.
  app = {
    function_name    = "IncognitoChatBot"
    table_name       = "IncognitoChatBot"
    invitations_name = "IncognitoChatBot-Invitations"
    secret_name      = "incognito-chat-bot-secrets"
    lambda_role_name = "incognito-chat-bot-lambda"
    lambda_log_group = "/aws/lambda/IncognitoChatBot"
    api_log_group    = "/aws/apigateway/incognito-chat-bot"
  }

  arns = {
    function     = "arn:${local.partition}:lambda:${local.region}:${local.account_id}:function:${local.app.function_name}"
    table        = "arn:${local.partition}:dynamodb:${local.region}:${local.account_id}:table/${local.app.table_name}"
    invitations  = "arn:${local.partition}:dynamodb:${local.region}:${local.account_id}:table/${local.app.invitations_name}"
    secret       = "arn:${local.partition}:secretsmanager:${local.region}:${local.account_id}:secret:${local.app.secret_name}-*"
    lambda_role  = "arn:${local.partition}:iam::${local.account_id}:role/${local.app.lambda_role_name}"
    log_group    = "arn:${local.partition}:logs:${local.region}:${local.account_id}:log-group"
    apigateway   = "arn:${local.partition}:apigateway:${local.region}::"
    state_bucket = aws_s3_bucket.state.arn
  }
}
