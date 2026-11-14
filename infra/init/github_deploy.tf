# GitHub Actions authenticates with short-lived OIDC credentials (no stored
# AWS keys). Only jobs that run in the configured GitHub environment can
# assume the deploy role, and the environment only accepts one branch (set by
# scripts/bootstrap.sh).
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
  name                 = "${local.prefix}-github-deploy"
  description          = "Assumed by GitHub Actions (${var.github_environment} environment) to deploy the app stack"
  assume_role_policy   = data.aws_iam_policy_document.deploy_trust.json
  max_session_duration = 3600
}

resource "aws_iam_role_policy_attachment" "deploy" {
  role       = aws_iam_role.deploy.name
  policy_arn = aws_iam_policy.deploy.arn
}

# Least privilege: the deploy role can manage only the app stack's own
# resources (by name prefix) and read/write only the app/ prefix of the state
# bucket.
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

  # The media bucket's configuration, not its contents: the deploy role can't
  # read, write or delete the encrypted attachments.
  statement {
    sid = "MediaBucket"
    actions = [
      "s3:CreateBucket",
      "s3:DeleteBucket",
      "s3:ListBucket",
      "s3:GetBucket*",
      "s3:PutBucket*",
      "s3:DeleteBucketPolicy",
      "s3:GetAccelerateConfiguration",
      "s3:GetEncryptionConfiguration",
      "s3:PutEncryptionConfiguration",
      "s3:GetLifecycleConfiguration",
      "s3:PutLifecycleConfiguration",
      "s3:GetReplicationConfiguration",
    ]
    resources = [local.arns.media_buckets]
  }

  # Functions, their versions and their function URLs.
  statement {
    sid       = "Lambda"
    actions   = ["lambda:*"]
    resources = [local.arns.functions, "${local.arns.functions}:*"]
  }

  statement {
    sid       = "DynamoDB"
    actions   = ["dynamodb:*"]
    resources = [local.arns.tables, "${local.arns.tables}/*"]
  }

  # Secrets and settings. Values are written through Terraform write-only
  # arguments, so they never land in state or plan files.
  statement {
    sid = "Parameters"
    actions = [
      "ssm:PutParameter",
      "ssm:GetParameter",
      "ssm:GetParameters",
      "ssm:DeleteParameter",
      "ssm:AddTagsToResource",
      "ssm:RemoveTagsFromResource",
      "ssm:ListTagsForResource",
    ]
    resources = [local.arns.parameters]
  }

  # Doesn't support resource-level permissions.
  statement {
    sid       = "ParametersDescribe"
    actions   = ["ssm:DescribeParameters"]
    resources = ["*"]
  }

  statement {
    sid       = "LogGroups"
    actions   = ["logs:*"]
    resources = [local.arns.log_groups, "${local.arns.log_groups}:*"]
  }

  # Doesn't support resource-level permissions.
  statement {
    sid       = "LogsDescribe"
    actions   = ["logs:DescribeLogGroups"]
    resources = ["*"]
  }

  # The WebSocket API. API IDs are random, so this can't be narrowed by name.
  statement {
    sid = "ApiGateway"
    actions = [
      "apigateway:GET",
      "apigateway:POST",
      "apigateway:PUT",
      "apigateway:PATCH",
      "apigateway:DELETE",
      "apigateway:TagResource",
      "apigateway:UntagResource",
    ]
    resources = [
      "${local.arns.apigateway}/apis",
      "${local.arns.apigateway}/apis/*",
      "${local.arns.apigateway}/tags/*",
    ]
  }

  # The deploy role may create the functions' execution roles and change their
  # policies only while the permissions boundary below is attached. That
  # boundary caps what any function can ever do, so the deploy role can't use
  # a role it creates to escalate its own privileges.
  statement {
    sid = "LambdaRolesWithBoundary"
    actions = [
      "iam:CreateRole",
      "iam:PutRolePolicy",
      "iam:DeleteRolePolicy",
      "iam:AttachRolePolicy",
      "iam:DetachRolePolicy",
      "iam:PutRolePermissionsBoundary",
    ]
    resources = [local.arns.lambda_roles]

    condition {
      test     = "StringEquals"
      variable = "iam:PermissionsBoundary"
      values   = [aws_iam_policy.lambda_boundary.arn]
    }
  }

  statement {
    sid = "LambdaRolesManage"
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
    resources = [local.arns.lambda_roles]
  }

  statement {
    sid       = "PassLambdaRolesToLambdaOnly"
    actions   = ["iam:PassRole"]
    resources = [local.arns.lambda_roles]

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

  # Code signing (lambda_signing.tf): upload builds, have them signed, and
  # deploy the signed copies. Bucket settings, the profile and the code
  # signing config stay out of reach.
  statement {
    sid = "BuildsBucket"
    actions = [
      "s3:ListBucket",
      "s3:ListBucketVersions",
      "s3:GetBucketLocation",
      "s3:GetBucketVersioning",
    ]
    resources = [aws_s3_bucket.builds.arn]
  }

  statement {
    sid = "Builds"
    actions = [
      "s3:GetObject",
      "s3:GetObjectVersion",
      "s3:GetObjectTagging",
      "s3:GetObjectVersionTagging",
      "s3:PutObject",
      "s3:PutObjectTagging",
      # Terraform deletes every version of a removed build.
      "s3:DeleteObject",
      "s3:DeleteObjectVersion",
    ]
    resources = ["${aws_s3_bucket.builds.arn}/*"]
  }

  statement {
    sid       = "SignBuilds"
    actions   = ["signer:StartSigningJob", "signer:GetSigningProfile"]
    resources = [aws_signer_signing_profile.lambda.arn]
  }

  statement {
    sid       = "SigningJobs"
    actions   = ["signer:DescribeSigningJob", "signer:GetRevocationStatus"]
    resources = ["arn:${local.partition}:signer:${local.region}:${local.account_id}:/signing-jobs/*"]
  }

  statement {
    sid       = "UseCodeSigningConfig"
    actions   = ["lambda:GetCodeSigningConfig", "lambda:PutFunctionCodeSigningConfig"]
    resources = [aws_lambda_code_signing_config.lambda.arn]
  }

  # Functions are created, and stay, with this code signing config only.
  statement {
    sid       = "KeepCodeSigningConfig"
    effect    = "Deny"
    actions   = ["lambda:CreateFunction", "lambda:PutFunctionCodeSigningConfig"]
    resources = ["*"]

    condition {
      test     = "StringNotEquals"
      variable = "lambda:CodeSigningConfigArn"
      values   = [aws_lambda_code_signing_config.lambda.arn]
    }
  }

  statement {
    sid       = "KeepCodeSigningAttached"
    effect    = "Deny"
    actions   = ["lambda:DeleteFunctionCodeSigningConfig"]
    resources = ["*"]
  }
}

resource "aws_iam_policy" "deploy" {
  name        = "${local.prefix}-github-deploy"
  description = "Least-privilege permissions for deploying the Incognito Chat app stack (${var.github_environment})"
  policy      = data.aws_iam_policy_document.deploy.json
}

# The edge in front of the API: CloudFront and its firewall. A policy of its
# own, because one managed policy can't hold everything.
data "aws_iam_policy_document" "deploy_edge" {
  # CloudFront in front of the API. Distribution and access-control IDs are
  # random, so this can't be narrowed by name.
  statement {
    sid = "CloudFront"
    actions = [
      "cloudfront:CreateDistribution",
      "cloudfront:CreateDistributionWithTags",
      "cloudfront:GetDistribution",
      "cloudfront:GetDistributionConfig",
      "cloudfront:UpdateDistribution",
      "cloudfront:DeleteDistribution",
      "cloudfront:TagResource",
      "cloudfront:UntagResource",
      "cloudfront:ListTagsForResource",
      "cloudfront:CreateOriginAccessControl",
      "cloudfront:GetOriginAccessControl",
      "cloudfront:GetOriginAccessControlConfig",
      "cloudfront:UpdateOriginAccessControl",
      "cloudfront:DeleteOriginAccessControl",
    ]
    resources = [
      "arn:${local.partition}:cloudfront::${local.account_id}:distribution/*",
      "arn:${local.partition}:cloudfront::${local.account_id}:origin-access-control/*",
    ]
  }

  # AWS's managed cache and origin-request policies, looked up by name.
  statement {
    sid = "CloudFrontPolicies"
    actions = [
      "cloudfront:ListCachePolicies",
      "cloudfront:GetCachePolicy",
      "cloudfront:ListOriginRequestPolicies",
      "cloudfront:GetOriginRequestPolicy",
      "cloudfront:ListOriginAccessControls",
    ]
    resources = ["*"]
  }

  # The firewall in front of CloudFront: web ACLs named with the prefix
  # (they live in us-east-1, as CloudFront's do), and AWS's managed rules.
  statement {
    sid = "Firewall"
    actions = [
      "wafv2:CreateWebACL",
      "wafv2:GetWebACL",
      "wafv2:UpdateWebACL",
      "wafv2:DeleteWebACL",
      "wafv2:TagResource",
      "wafv2:UntagResource",
      "wafv2:ListTagsForResource",
    ]
    resources = [
      "arn:${local.partition}:wafv2:us-east-1:${local.account_id}:global/webacl/${local.prefix}-*/*",
      "arn:${local.partition}:wafv2:us-east-1:${local.account_id}:global/managedruleset/*/*",
    ]
  }
}

resource "aws_iam_policy" "deploy_edge" {
  name        = "${local.prefix}-github-deploy-edge"
  description = "Permissions for deploying CloudFront and its firewall in front of the Incognito Chat API (${var.github_environment})"
  policy      = data.aws_iam_policy_document.deploy_edge.json
}

resource "aws_iam_role_policy_attachment" "deploy_edge" {
  role       = aws_iam_role.deploy.name
  policy_arn = aws_iam_policy.deploy_edge.arn
}

# The most any function's execution role can ever be allowed to do. Each
# function gets a narrower policy of its own in the app stack.
data "aws_iam_policy_document" "lambda_boundary" {
  statement {
    sid       = "Logs"
    actions   = ["logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["${local.arns.log_groups}:*"]
  }

  statement {
    sid = "DynamoDB"
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
    resources = [local.arns.tables, "${local.arns.tables}/index/*"]
  }

  statement {
    sid       = "Parameters"
    actions   = ["ssm:GetParameter", "ssm:GetParameters"]
    resources = [local.arns.parameters]
  }

  statement {
    sid       = "PushToSockets"
    actions   = ["execute-api:ManageConnections"]
    resources = [local.arns.ws_management]
  }

  statement {
    sid       = "InvokeNotify"
    actions   = ["lambda:InvokeFunction"]
    resources = [local.arns.notify]
  }

  # Encrypted attachments (presigned uploads and downloads, deletion).
  statement {
    sid       = "Media"
    actions   = ["s3:PutObject", "s3:GetObject", "s3:DeleteObject"]
    resources = ["${local.arns.media_buckets}/*"]
  }
}

resource "aws_iam_policy" "lambda_boundary" {
  name        = "${local.prefix}-lambda-boundary"
  description = "Permissions boundary for every Incognito Chat function role (${var.github_environment})"
  policy      = data.aws_iam_policy_document.lambda_boundary.json
}
