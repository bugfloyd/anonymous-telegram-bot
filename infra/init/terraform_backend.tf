resource "aws_s3_bucket" "terraform_state" {
  bucket = var.terraform_state_bucket
}

resource "aws_dynamodb_table" "terraform_lock" {
  name         = var.terraform_state_lock_dynamodb_table
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "LockID"

  attribute {
    name = "LockID"
    type = "S"
  }

  tags = {
    Name = "TerraformStateLocking"
  }
}

resource "aws_iam_policy" "terraform_backend_access_policy" {
  name        = "TerraformBackendAccessPolicy"
  path        = "/"
  description = "Policy for allowing access to terraform backend S3 bucket and DynamoDB state lock table"

  policy = <<EOF
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": "s3:ListBucket",
      "Resource": "${aws_s3_bucket.terraform_state.arn}"
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject"],
      "Resource": "${aws_s3_bucket.terraform_state.arn}/*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:DescribeTable",
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:DeleteItem"
      ],
      "Resource": "${aws_dynamodb_table.terraform_lock.arn}"
    }
  ]
}
EOF
}