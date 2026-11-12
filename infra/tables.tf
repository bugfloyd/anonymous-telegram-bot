# Three tables, split because point-in-time recovery, TTL and IAM are set per
# table. All use pk/sk string keys and expire items through the "e" attribute
# (Unix seconds, rounded to the hour where it could reveal timing). TTL deletes
# can lag by days, so reads also filter expired items.
#
# Point-in-time recovery stays off everywhere: it would keep deleted data
# restorable for up to 35 days. Encryption at rest uses the AWS-owned key: the
# data that matters is already end-to-end encrypted.
locals {
  tables = {
    # Accounts, devices, device lists, links and their bundles, the mailbox
    # registry, invitations.
    directory = {}
    # Undelivered ciphertext, one item per message per device. No sender, no account.
    messages = {}
    # Socket connections, rate counters, login nonces, spent tokens.
    ephemeral = {}
  }
}

resource "aws_dynamodb_table" "table" {
  for_each = local.tables

  name                        = "${local.prefix}-${each.key}"
  billing_mode                = "PAY_PER_REQUEST"
  hash_key                    = "pk"
  range_key                   = "sk"
  deletion_protection_enabled = var.deletion_protection

  attribute {
    name = "pk"
    type = "S"
  }

  attribute {
    name = "sk"
    type = "S"
  }

  ttl {
    attribute_name = "e"
    enabled        = true
  }

  point_in_time_recovery {
    enabled = false
  }

  on_demand_throughput {
    max_read_request_units  = var.table_max_read_units
    max_write_request_units = var.table_max_write_units
  }
}
