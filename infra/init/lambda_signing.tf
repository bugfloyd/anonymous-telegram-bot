# Lambda code signing. CI uploads each function build here, has AWS Signer
# sign it with this profile, and deploys the signed copy. The functions run
# only code signed by this profile (Enforce). Everything here is applied by
# an account admin, so the deploy role can sign but can't change the
# profile, the bucket's settings or the rule, nor deploy without the rule
# (github_deploy.tf).

# Signer needs a versioned source bucket: each job signs one object version.
# Builds hold no user data; the name ends in random characters, not the
# account ID.
resource "aws_s3_bucket" "builds" {
  bucket_prefix = "${local.prefix}-builds-"
  force_destroy = true
}

resource "aws_s3_bucket_versioning" "builds" {
  bucket = aws_s3_bucket.builds.id

  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "builds" {
  bucket = aws_s3_bucket.builds.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_public_access_block" "builds" {
  bucket = aws_s3_bucket.builds.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "builds" {
  bucket = aws_s3_bucket.builds.id

  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

# Lambda keeps its own copy of deployed code, so old builds can go.
resource "aws_s3_bucket_lifecycle_configuration" "builds" {
  bucket = aws_s3_bucket.builds.id

  rule {
    id     = "expire-old-builds"
    status = "Enabled"

    filter {}

    noncurrent_version_expiration {
      noncurrent_days = 30
    }

    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }

  rule {
    id     = "expire-signed-copies"
    status = "Enabled"

    filter {
      prefix = "signed/"
    }

    expiration {
      days = 90
    }
  }

  depends_on = [aws_s3_bucket_versioning.builds]
}

data "aws_iam_policy_document" "builds_bucket" {
  statement {
    sid     = "DenyInsecureTransport"
    effect  = "Deny"
    actions = ["s3:*"]
    resources = [
      aws_s3_bucket.builds.arn,
      "${aws_s3_bucket.builds.arn}/*",
    ]

    principals {
      type        = "*"
      identifiers = ["*"]
    }

    condition {
      test     = "Bool"
      variable = "aws:SecureTransport"
      values   = ["false"]
    }
  }
}

resource "aws_s3_bucket_policy" "builds" {
  bucket = aws_s3_bucket.builds.id
  policy = data.aws_iam_policy_document.builds_bucket.json

  depends_on = [aws_s3_bucket_public_access_block.builds]
}

# Profile names allow letters, digits and underscores only. A cancelled
# profile's name can't be taken again, so the name ends in a unique suffix.
# Signatures stay valid for 135 months, Signer's default and maximum; Lambda
# checks them only when code is deployed.
resource "aws_signer_signing_profile" "lambda" {
  name_prefix = "${replace(local.prefix, "-", "_")}_lambda_"
  platform_id = "AWSLambda-SHA384-ECDSA"

  signature_validity_period {
    value = 135
    type  = "MONTHS"
  }
}

resource "aws_lambda_code_signing_config" "lambda" {
  description = "Incognito Chat (${var.github_environment}): run only code signed by CI"

  allowed_publishers {
    signing_profile_version_arns = [aws_signer_signing_profile.lambda.version_arn]
  }

  policies {
    untrusted_artifact_on_deployment = "Enforce"
  }
}
