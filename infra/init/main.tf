locals {
  workspace_prefixes = {
    default     = "dev"
    development = "dev"
    production  = "prod"
  }
}

provider "aws" {
  region  = var.init_aws_region
  profile = var.init_aws_profile[terraform.workspace]
}

# S3 bucket to store lambda function codes
resource "aws_s3_bucket" "lambda_bucket" {
  bucket = "${local.workspace_prefixes[terraform.workspace]}.${var.init_lambda_bucket}"
}