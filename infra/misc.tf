# resource "aws_config_config_rule" "root_account_mfa_enabled" {
#   name = "root-account-mfa-enabled"
#   source {
#     owner             = "AWS"
#     source_identifier = "ROOT_ACCOUNT_MFA_ENABLED"
#   }
# }