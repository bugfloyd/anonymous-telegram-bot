resource "aws_dynamodb_table" "invitations" {
  name         = "AnonymousBot_Invitations"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "ItemID"

  attribute {
    name = "ItemID"
    type = "S"
  }

  attribute {
    name = "UserID"
    type = "S"
  }

  global_secondary_index {
    name            = "UserID-GSI"
    hash_key        = "UserID"
    range_key       = "ItemID"
    projection_type = "ALL"
  }

  lifecycle {
    prevent_destroy = false
  }
}

resource "aws_iam_policy" "lambda_dynamodb_invitations_policy" {
  name        = "AnonymousInvitationsDynamoDBLambdaPolicy"
  description = "Policy to allow Lambda function to manage invitations DynamoDB table"

  policy = jsonencode({
    Version = "2012-10-17",
    Statement = [
      {
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem",
          "dynamodb:Query",
          "dynamodb:BatchGetItem",
          "dynamodb:BatchWriteItem",
          "dynamodb:DescribeTable",
        ],
        Effect   = "Allow",
        Resource = aws_dynamodb_table.invitations.arn
      },
      {
        Action = [
          "dynamodb:Query",
        ],
        Effect = "Allow",
        Resource = [
          "${aws_dynamodb_table.invitations.arn}/index/UserID-GSI"
        ]
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "rest_backend_lambda_invitations_dynamodb_policy_attachment" {
  role       = aws_iam_role.lambda_exec_role.name
  policy_arn = aws_iam_policy.lambda_dynamodb_invitations_policy.arn
}