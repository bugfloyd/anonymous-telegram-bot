terraform {
  # >= 1.11 for ephemeral values and write-only arguments, which keep secrets
  # out of state and plan files.
  required_version = ">= 1.11"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.7"
    }
  }

  # Partial configuration: bucket and region are passed with -backend-config
  # by CI (the bucket is created by infra/init).
  backend "s3" {
    key          = "app/terraform.tfstate"
    encrypt      = true
    use_lockfile = true
  }
}
