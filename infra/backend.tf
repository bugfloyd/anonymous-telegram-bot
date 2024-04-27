terraform {
  backend "s3" {
    key            = "anonymous-chat-state/terraform.tfstate"
    encrypt        = true
    dynamodb_table = "anonymous-chatbot-terraform-state-lock"
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.45"
    }
  }

  required_version = ">= 1.8"
}
