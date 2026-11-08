terraform {
  required_version = ">= 1.11"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }

  # Partial configuration: bucket and region are supplied by scripts/bootstrap.sh.
  # On the very first run the bucket doesn't exist yet, so the script applies
  # with local state and then migrates that state into the bucket.
  backend "s3" {
    key          = "bootstrap/terraform.tfstate"
    encrypt      = true
    use_lockfile = true
  }
}
