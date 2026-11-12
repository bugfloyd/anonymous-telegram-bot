# Bootstrap stack: everything CI/CD needs before it can deploy the app stack
# (../). It's applied by an account admin via scripts/bootstrap.sh, never by CI,
# so the pipeline can't widen its own permissions. One AWS account per
# environment (dev, production).

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "incognitochat"
      Environment = var.github_environment
      Stack       = "bootstrap"
      ManagedBy   = "terraform"
    }
  }
}

data "aws_caller_identity" "current" {}
data "aws_partition" "current" {}

locals {
  account_id = data.aws_caller_identity.current.account_id
  partition  = data.aws_partition.current.partition
  region     = var.aws_region

  # Every app resource is named "<prefix>-…". The deploy role may only touch
  # names with this prefix, so keep both stacks in sync (../main.tf).
  prefix = "incognitochat-${var.github_environment}"

  # SSM parameters live under this path.
  parameter_path = "/incognitochat/${var.github_environment}"

  arns = {
    functions  = "arn:${local.partition}:lambda:${local.region}:${local.account_id}:function:${local.prefix}-*"
    notify     = "arn:${local.partition}:lambda:${local.region}:${local.account_id}:function:${local.prefix}-notify"
    tables     = "arn:${local.partition}:dynamodb:${local.region}:${local.account_id}:table/${local.prefix}-*"
    parameters = "arn:${local.partition}:ssm:${local.region}:${local.account_id}:parameter${local.parameter_path}/*"
    # Function roles have their own prefix, so the deploy role ("<prefix>-github-deploy")
    # can never match it and change itself.
    lambda_roles  = "arn:${local.partition}:iam::${local.account_id}:role/${local.prefix}-fn-*"
    log_groups    = "arn:${local.partition}:logs:${local.region}:${local.account_id}:log-group:/aws/lambda/${local.prefix}-*"
    apigateway    = "arn:${local.partition}:apigateway:${local.region}::"
    ws_management = "arn:${local.partition}:execute-api:${local.region}:${local.account_id}:*/*/*/@connections/*"
    state_bucket  = aws_s3_bucket.state.arn
  }
}
