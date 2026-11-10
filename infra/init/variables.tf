variable "aws_region" {
  description = "AWS region for the state bucket and the app resources the deploy role may manage"
  type        = string
}

variable "state_bucket_name" {
  description = "Name of the S3 bucket that stores Terraform state for both stacks"
  type        = string
}

variable "github_oidc_subject_prefix" {
  description = <<-EOT
    Prefix of the OIDC "sub" claim GitHub issues for this repository, as reported by
    GET /repos/<owner>/<repo>/actions/oidc/customization/sub (sub_claim_prefix):
    "repo:<owner>/<repo>", or with immutable subjects "repo:<owner>@<owner-id>/<repo>@<repo-id>".
    scripts/bootstrap.sh looks it up so it always matches what GitHub actually sends.
  EOT
  type        = string

  validation {
    condition     = can(regex("^repo:[^:]+/[^:]+$", var.github_oidc_subject_prefix))
    error_message = "github_oidc_subject_prefix must look like repo:<owner>/<repo> (optionally with @<id> suffixes)."
  }
}

variable "github_environment" {
  description = "GitHub environment whose jobs may assume the deploy role. It also names every resource (incognitochat-<environment>-…)."
  type        = string
  default     = "production"

  validation {
    condition     = can(regex("^[a-z][a-z0-9]{1,15}$", var.github_environment))
    error_message = "github_environment must be 2-16 lowercase letters or digits, starting with a letter (e.g. dev, production)."
  }
}
