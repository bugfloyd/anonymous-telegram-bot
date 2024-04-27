variable "init_aws_region" {
  description = "The AWS region to create resources in"
  type        = string
}

variable "init_aws_profile" {
  description = "AWS profile to be used"
  type = map(string)
  default = {
    development = "dev"
    production  = "prod"
  }
}

variable "init_lambda_bucket" {
  description = "The AWS S3 bucket for storing lambda function codes"
  type        = string
}