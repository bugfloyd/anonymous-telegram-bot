# CloudFront in front of the HTTPS API, and optionally a firewall in front of
# CloudFront. Floods can then be limited per IP address at the edge, before
# they reach Lambda, and a blocked request costs no invocation.
#
# var.api_access moves the API behind it in steps:
#
#   direct           the app calls the function URL (CloudFront exists, unused)
#   cloudfront       the app calls CloudFront; the function URL still answers
#   cloudfront-only  the function URL answers CloudFront alone: it requires
#                    AWS's signature, which CloudFront adds to each request
#
# Nothing is logged here: no access logs on the distribution, and no sampled
# requests in the firewall (they would hold IP addresses).

# CloudFront's firewalls live in us-east-1, whatever the app's region.
provider "aws" {
  alias  = "global"
  region = "us-east-1"

  default_tags {
    tags = {
      Project     = "incognitochat"
      Environment = var.environment
      Stack       = "app"
      ManagedBy   = "terraform"
    }
  }
}

locals {
  api_behind_cloudfront = var.api_access != "direct"
  api_locked            = var.api_access == "cloudfront-only"
  # "https://<id>.lambda-url.<region>.on.aws/" -> the host.
  api_origin_host = trimsuffix(trimprefix(aws_lambda_function_url.api.function_url, "https://"), "/")
}

# The API's answers are never cached, and every header of the request goes on
# to the function (except Host, which must be the function URL's own).
data "aws_cloudfront_cache_policy" "disabled" {
  name = "Managed-CachingDisabled"
}

data "aws_cloudfront_origin_request_policy" "all_but_host" {
  name = "Managed-AllViewerExceptHostHeader"
}

# CloudFront signs its requests to the function URL (SigV4), so the URL can
# require a signature and refuse everyone else without running the function.
resource "aws_cloudfront_origin_access_control" "api" {
  name                              = "${local.prefix}-api"
  description                       = "Signs CloudFront's requests to the api function URL"
  origin_access_control_origin_type = "lambda"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_cloudfront_distribution" "api" {
  enabled         = true
  comment         = "Incognito Chat API (${var.environment})"
  http_version    = "http2and3"
  is_ipv6_enabled = true
  # The cheapest edge locations; every viewer is still served.
  price_class = "PriceClass_100"
  web_acl_id  = var.firewall ? aws_wafv2_web_acl.api[0].arn : null

  origin {
    origin_id                = "api"
    domain_name              = local.api_origin_host
    origin_access_control_id = local.api_locked ? aws_cloudfront_origin_access_control.api.id : null

    custom_origin_config {
      http_port              = 80
      https_port             = 443
      origin_protocol_policy = "https-only"
      origin_ssl_protocols   = ["TLSv1.2"]
    }
  }

  default_cache_behavior {
    target_origin_id         = "api"
    viewer_protocol_policy   = "https-only"
    allowed_methods          = ["GET", "HEAD", "OPTIONS", "PUT", "POST", "PATCH", "DELETE"]
    cached_methods           = ["GET", "HEAD"]
    cache_policy_id          = data.aws_cloudfront_cache_policy.disabled.id
    origin_request_policy_id = data.aws_cloudfront_origin_request_policy.all_but_host.id
    compress                 = true
  }

  restrictions {
    geo_restriction {
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }
}

# CloudFront may call the function URL, for this distribution only. Both
# permissions are needed, as for a public URL (functions.tf).
resource "aws_lambda_permission" "cloudfront_url" {
  statement_id  = "CloudFrontUrl"
  action        = "lambda:InvokeFunctionUrl"
  function_name = aws_lambda_function.fn["api"].function_name
  principal     = "cloudfront.amazonaws.com"
  source_arn    = aws_cloudfront_distribution.api.arn
}

resource "aws_lambda_permission" "cloudfront_invoke" {
  statement_id  = "CloudFrontInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.fn["api"].function_name
  principal     = "cloudfront.amazonaws.com"
  source_arn    = aws_cloudfront_distribution.api.arn
}

# The firewall: one rule, a limit on requests per IP address. Many people can
# share one address (a mobile carrier, a VPN), so the limit is far above what
# one person's app sends. Costs a few dollars a month, so it is optional.
resource "aws_wafv2_web_acl" "api" {
  count    = var.firewall ? 1 : 0
  provider = aws.global

  name        = "${local.prefix}-api"
  description = "Limits requests per IP address to the Incognito Chat API (${var.environment})"
  scope       = "CLOUDFRONT"

  default_action {
    allow {}
  }

  rule {
    name     = "rate-per-ip"
    priority = 1

    action {
      block {}
    }

    statement {
      rate_based_statement {
        limit                 = var.firewall_rate_limit
        aggregate_key_type    = "IP"
        evaluation_window_sec = 300
      }
    }

    visibility_config {
      cloudwatch_metrics_enabled = true
      metric_name                = "${local.prefix}-api-rate-per-ip"
      sampled_requests_enabled   = false
    }
  }

  visibility_config {
    cloudwatch_metrics_enabled = true
    metric_name                = "${local.prefix}-api"
    sampled_requests_enabled   = false
  }
}
