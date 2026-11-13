output "deploy_role_arn" {
  description = "Role GitHub Actions assumes to deploy (stored as a GitHub environment secret)"
  value       = aws_iam_role.deploy.arn
}

output "deploy_policy_arn" {
  description = "Managed policy of the deploy role"
  value       = aws_iam_policy.deploy.arn
}

output "lambda_boundary_policy_arn" {
  description = "Permissions boundary the app stack must attach to every function role"
  value       = aws_iam_policy.lambda_boundary.arn
}

output "state_bucket_name" {
  description = "S3 bucket holding Terraform state"
  value       = aws_s3_bucket.state.bucket
}

output "builds_bucket" {
  description = "Versioned bucket CI uploads function builds to for signing (stored as a GitHub environment secret)"
  value       = aws_s3_bucket.builds.bucket
}

output "signing_profile_name" {
  description = "AWS Signer profile CI signs function builds with (stored as a GitHub environment secret)"
  value       = aws_signer_signing_profile.lambda.name
}

output "code_signing_config_arn" {
  description = "Code signing config every function must have (stored as a GitHub environment secret)"
  value       = aws_lambda_code_signing_config.lambda.arn
}
