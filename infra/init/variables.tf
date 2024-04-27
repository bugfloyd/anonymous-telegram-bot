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

variable "github_repo" {
  description = "Github repo to be used to allow github actions to have access to the AWS account via OIDC in this format: <GITHUB_OWNER>/<GITHUB_REPO>"
  type        = string
}