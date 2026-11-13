# Encrypted attachments (design phase 5). The app uploads ciphertext straight
# here with presigned URLs from the api function, and the recipient deletes
# it once fetched. Nothing in here can be read without the key in the
# encrypted message. The name ends in random characters rather than the
# account ID, since presigned URLs and the app's security policy show it.
resource "random_id" "media" {
  byte_length = 6
}

resource "aws_s3_bucket" "media" {
  bucket        = "${local.prefix}-media-${random_id.media.hex}"
  force_destroy = !var.deletion_protection
}

resource "aws_s3_bucket_public_access_block" "media" {
  bucket                  = aws_s3_bucket.media.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_ownership_controls" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    object_ownership = "BucketOwnerEnforced"
  }
}

resource "aws_s3_bucket_server_side_encryption_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Undelivered messages wait 30 days; so do their attachments, plus a day.
# No versioning, so a deleted attachment is gone.
resource "aws_s3_bucket_lifecycle_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    id     = "expire"
    status = "Enabled"
    filter {}
    expiration {
      days = 31
    }
    abort_incomplete_multipart_upload {
      days_after_initiation = 1
    }
  }
}

# Browsers upload and download from the app's origin only.
resource "aws_s3_bucket_cors_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  cors_rule {
    allowed_origins = var.client_origins
    allowed_methods = ["PUT", "GET"]
    allowed_headers = ["content-type"]
    max_age_seconds = 3600
  }
}

data "aws_iam_policy_document" "media" {
  statement {
    sid       = "TlsOnly"
    effect    = "Deny"
    actions   = ["s3:*"]
    resources = [aws_s3_bucket.media.arn, "${aws_s3_bucket.media.arn}/*"]
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

resource "aws_s3_bucket_policy" "media" {
  bucket     = aws_s3_bucket.media.id
  policy     = data.aws_iam_policy_document.media.json
  depends_on = [aws_s3_bucket_public_access_block.media]
}
