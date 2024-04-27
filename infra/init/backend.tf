terraform {
  backend "s3" {
    key            = "anonymous-chat-state/init/terraform.tfstate"
    encrypt        = true
    dynamodb_table = "anonymous-chatbot-terraform-state-lock"
  }
}