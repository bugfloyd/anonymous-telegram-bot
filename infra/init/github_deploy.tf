# GitHub Actions authenticates with short-lived OIDC credentials (no stored
# AWS keys). Only jobs that run in the configured GitHub environment can
# assume the deploy role, and the environment only accepts the main branch.
resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://token.actions.githubusercontent.com"
  client_id_list = ["sts.amazonaws.com"]
}

data "aws_iam_policy_document" "deploy_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]

    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:aud"
      values   = ["sts.amazonaws.com"]
    }

    condition {
      test     = "StringEquals"
      variable = "token.actions.githubusercontent.com:sub"
      values   = ["${var.github_oidc_subject_prefix}:environment:${var.github_environment}"]
    }
  }
}

resource "aws_iam_role" "deploy" {
  name                 = "incognito-chat-bot-github-deploy"
  description          = "Assumed by GitHub Actions (${var.github_environment} environment) to deploy the app stack"
  assume_role_policy   = data.aws_iam_policy_document.deploy_trust.json
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "deploy" {
  role       = aws_iam_role.deploy.name
  policy_arn = aws_iam_policy.deploy.arn
}

# Least privilege: the deploy role can manage only the app stack's own
# resources (by name) and read/write only the app/ prefix of the state bucket.
data "aws_iam_policy_document" "deploy" {
  statement {
    sid       = "StateBucketList"
    actions   = ["s3:ListBucket"]
    resources = [local.arns.state_bucket]
  }

  statement {
    sid       = "AppStateReadWrite"
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${local.arns.state_bucket}/app/*"]
  }

  statement {
    sid       = "Lambda"
    actions   = ["lambda:*"]
    resources = [local.arns.function, "${local.arns.function}:*"]
  }

  statement {
    sid       = "DynamoDB"
    actions   = ["dynamodb:*"]
    resources = [local.arns.table, "${local.arns.table}/*"]
  }

  statement {
    sid       = "SecretsManager"
    actions   = ["secretsmanager:*"]
    resources = [local.arns.secret]
  }

  statement {
    sid     = "AppLogGroups"
    actions = ["logs:*"]
    resources = flatten([
      for name in [local.app.lambda_log_group, local.app.api_log_group] : [
        "${local.arns.log_group}:${name}",
        "${local.arns.log_group}:${name}:*",
      ]
    ])
  }

  # These calls don't support resource-level permissions. The log delivery
  # calls are needed to configure API Gateway (HTTP API) access logging.
  statement {
    sid = "LogsAccountLevel"
    actions = [
      "logs:DescribeLogGroups",
      "logs:CreateLogDelivery",
      "logs:GetLogDelivery",
      "logs:UpdateLogDelivery",
      "logs:DeleteLogDelivery",
      "logs:ListLogDeliveries",
      "logs:PutResourcePolicy",
      "logs:DescribeResourcePolicies",
    ]
    resources = ["*"]
  }

  statement {
    sid = "ApiGateway"
    actions = [
      "apigateway:GET",
      "apigateway:POST",
      "apigateway:PUT",
      "apigateway:PATCH",
      "apigateway:DELETE",
      # Checked separately when resources (e.g. stages) are created with tags.
      "apigateway:TagResource",
      "apigateway:UntagResource",
    ]
    resources = [
      "${local.arns.apigateway}/apis",
      "${local.arns.apigateway}/apis/*",
      "${local.arns.apigateway}/tags/*",
    ]
  }

  # The deploy role may create the Lambda execution role and change its
  # policies only while the permissions boundary below is attached. That
  # boundary caps what the Lambda can ever do, so the deploy role can't use
  # a role it creates to escalate its own privileges.
  statement {
    sid = "LambdaRoleWithBoundary"
    actions = [
      "iam:CreateRole",
      "iam:PutRolePolicy",
      "iam:DeleteRolePolicy",
      "iam:AttachRolePolicy",
      "iam:DetachRolePolicy",
      "iam:PutRolePermissionsBoundary",
    ]
    resources = [local.arns.lambda_role]

    condition {
      test     = "StringEquals"
      variable = "iam:PermissionsBoundary"
      values   = [aws_iam_policy.lambda_boundary.arn]
    }
  }

  statement {
    sid = "LambdaRoleManage"
    actions = [
      "iam:GetRole",
      "iam:DeleteRole",
      "iam:UpdateRole",
      "iam:UpdateAssumeRolePolicy",
      "iam:TagRole",
      "iam:UntagRole",
      "iam:ListRoleTags",
      "iam:GetRolePolicy",
      "iam:ListRolePolicies",
      "iam:ListAttachedRolePolicies",
      "iam:ListInstanceProfilesForRole",
    ]
    resources = [local.arns.lambda_role]
  }

  statement {
    sid       = "PassLambdaRoleToLambdaOnly"
    actions   = ["iam:PassRole"]
    resources = [local.arns.lambda_role]

    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["lambda.amazonaws.com"]
    }
  }

  statement {
    sid       = "KeepBoundaryAttached"
    effect    = "Deny"
    actions   = ["iam:DeleteRolePermissionsBoundary"]
    resources = ["*"]
  }
}

resource "aws_iam_policy" "deploy" {
  name        = "incognito-chat-bot-github-deploy"
  description = "Least-privilege permissions for deploying the incognito chat bot app stack"
  policy      = data.aws_iam_policy_document.deploy.json
}

# The most the Lambda execution role can ever be allowed to do.
data "aws_iam_policy_document" "lambda_boundary" {
  statement {
    sid       = "Logs"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${local.arns.log_group}:${local.app.lambda_log_group}:*"]
  }

  statement {
    sid       = "DynamoDB"
    actions   = ["dynamodb:GetItem", "dynamodb:PutItem", "dynamodb:UpdateItem", "dynamodb:Query"]
    resources = [local.arns.table, "${local.arns.table}/index/*"]
  }

  statement {
    sid       = "BotSecrets"
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [local.arns.secret]
  }
}

resource "aws_iam_policy" "lambda_boundary" {
  name        = "incognito-chat-bot-lambda-boundary"
  description = "Permissions boundary for the incognito chat bot Lambda execution role"
  policy      = data.aws_iam_policy_document.lambda_boundary.json
}
