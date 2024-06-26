variable "init_aws_region" {
  description = "The AWS region to create resources in"
  type        = string
}

variable "terraform_state_bucket" {
  description = "The AWS S3 bucket used to store terraform state"
  type        = string
}

variable "terraform_state_lock_dynamodb_table" {
  description = "The AWS DynamoDB table name to be used for terraform state lock"
  type        = string
  default     = "anonymous-chatbot-terraform-state-lock"
}

variable "init_lambda_bucket" {
  description = "The AWS S3 bucket for storing lambda function codes"
  type        = string
}

variable "github_repo" {
  description = "Github repo to be used to allow github actions to have access to the AWS account via OIDC in this format: <GITHUB_OWNER>/<GITHUB_REPO>"
  type        = string
}

