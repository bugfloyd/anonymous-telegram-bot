resource "aws_s3_bucket" "cloudtrail_bucket" {
  bucket = var.cloudtrail_bucket
}

resource "aws_s3_bucket_policy" "cloudtrail_bucket_policy" {
  bucket = aws_s3_bucket.cloudtrail_bucket.id

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Effect = "Allow",
        Principal = {
          Service = "cloudtrail.amazonaws.com"
        },
        Action   = "s3:GetBucketAcl",
        Resource = "arn:aws:s3:::${aws_s3_bucket.cloudtrail_bucket.id}"
      },
      {
        Effect = "Allow",
        Principal = {
          Service = "cloudtrail.amazonaws.com"
        },
        Action   = "s3:PutObject",
        Resource = "arn:aws:s3:::${aws_s3_bucket.cloudtrail_bucket.id}/AWSLogs/${data.aws_caller_identity.current.account_id}/*",
        Condition = {
          StringEquals = {
            "s3:x-amz-acl" = "bucket-owner-full-control"
          }
        }
      }
    ]
  })
}

resource "aws_cloudtrail" "main" {
  name                          = "whisperify-cloudtrail"
  s3_bucket_name                = aws_s3_bucket.cloudtrail_bucket.bucket
  include_global_service_events = true
  is_multi_region_trail         = true
  enable_logging                = true

  event_selector {
    read_write_type           = "All"
    include_management_events = true

    data_resource {
      type   = "AWS::S3::Object"
      values = ["arn:aws:s3:::"]
    }
  }

  cloud_watch_logs_group_arn = "${aws_cloudwatch_log_group.cloudtrail_log_group.arn}:*"
  cloud_watch_logs_role_arn  = aws_iam_role.cloudtrail_role.arn

  depends_on = [aws_s3_bucket_policy.cloudtrail_bucket_policy]
}

resource "aws_cloudwatch_log_group" "cloudtrail_log_group" {
  name = "/aws/cloudtrail/whisperify-cloudtrail"
}

resource "aws_iam_role" "cloudtrail_role" {
  name = "cloudtrail-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = "sts:AssumeRole",
        Effect = "Allow",
        Principal = {
          Service = "cloudtrail.amazonaws.com"
        }
      }
    ]
  })
}

resource "aws_iam_role_policy" "cloudtrail_policy" {
  name = "cloudtrail-policy"
  role = aws_iam_role.cloudtrail_role.id

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = [
          "logs:CreateLogStream",
          "logs:PutLogEvents"
        ],
        Effect   = "Allow",
        Resource = "${aws_cloudwatch_log_group.cloudtrail_log_group.arn}:*"
      }
    ]
  })
}

resource "aws_cloudwatch_metric_alarm" "root_account_login_alarm" {
  alarm_name          = "RootAccountLoginAlarm"
  comparison_operator = "GreaterThanOrEqualToThreshold"
  evaluation_periods  = "1"
  metric_name         = "RootLoginAttempts"
  namespace           = "CloudTrailMetrics"
  period              = "30"
  statistic           = "Sum"
  threshold           = "1"
  treat_missing_data  = "notBreaching"
  alarm_description   = "Alarm when root account logins occur"
  #   alarm_actions       = [aws_sns_topic.root_account_login_topic.arn]

  dimensions = {
    TrailName = aws_cloudtrail.main.name
  }

  depends_on = [aws_cloudtrail.main]
}

# resource "aws_sns_topic" "root_account_login_topic" {
#   name = "root-account-login-topic"
# }

# resource "aws_sns_topic_subscription" "root_account_login_subscription" {
#   topic_arn = aws_sns_topic.root_account_login_topic.arn
#   protocol  = "email"
#   endpoint  = "your-email@example.com"
# }

resource "aws_cloudwatch_log_metric_filter" "root_login_metric_filter" {
  name           = "RootLoginAttempts"
  log_group_name = aws_cloudwatch_log_group.cloudtrail_log_group.name
  pattern        = "{ ($.eventName = \"ConsoleLogin\") && ($.userIdentity.type = \"Root\") && ($.responseElements.ConsoleLogin = \"Success\") }"

  metric_transformation {
    name      = "RootLoginAttempts"
    namespace = "CloudTrailMetrics"
    value     = "1"
  }
}