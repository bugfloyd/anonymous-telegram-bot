variable "aws_region" {
  description = "AWS region for the state bucket and the app resources the deploy role may manage"
  type        = string
}

variable "state_bucket_name" {
  description = "Name of the S3 bucket that stores Terraform state for both stacks"
  type        = string
}

variable "github_repo" {
  description = "GitHub repository allowed to deploy, in the form <owner>/<repo>"
  type        = string

  validation {
    condition     = can(regex("^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$", var.github_repo))
    error_message = "github_repo must look like <owner>/<repo>."
  }
}

variable "github_environment" {
  description = "GitHub environment whose jobs may assume the deploy role"
  type        = string
  default     = "production"
}
