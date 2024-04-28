provider "aws" {
  region = var.init_aws_region
}

# S3 bucket to store lambda function codes
resource "aws_s3_bucket" "lambda_bucket" {
  bucket = var.init_lambda_bucket
}