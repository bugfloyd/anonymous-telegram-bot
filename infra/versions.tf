terraform {
  # >= 1.11 for ephemeral variables and write-only arguments, which keep the
  # bot token out of state and plan files.
  required_version = ">= 1.11"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }

  # Partial configuration: bucket and region are passed with -backend-config
  # (by CI, or locally from a gitignored backend_config.hcl).
  backend "s3" {
    key          = "app/terraform.tfstate"
    encrypt      = true
    use_lockfile = true
  }
}
