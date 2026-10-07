# DynamoDB table for rate-my-comms plus an IAM policy that grants the
# service exactly what it needs. Attach the policy to whatever role your
# pods assume.

variable "table_name" {
  type    = string
  default = "rate-my-comms"
}

variable "tags" {
  type    = map(string)
  default = {}
}

resource "aws_dynamodb_table" "votes" {
  name         = var.table_name
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "key"
  range_key    = "id"

  attribute {
    name = "key"
    type = "S"
  }

  attribute {
    name = "id"
    type = "S"
  }

  point_in_time_recovery {
    enabled = true
  }

  tags = var.tags
}

data "aws_iam_policy_document" "votes" {
  statement {
    actions = [
      "dynamodb:PutItem",
      "dynamodb:UpdateItem",
      "dynamodb:Query",
      "dynamodb:Scan",
    ]
    resources = [aws_dynamodb_table.votes.arn]
  }
}

resource "aws_iam_policy" "votes" {
  name   = "${var.table_name}-dynamodb"
  policy = data.aws_iam_policy_document.votes.json
  tags   = var.tags
}

output "table_name" {
  value = aws_dynamodb_table.votes.name
}

output "policy_arn" {
  value = aws_iam_policy.votes.arn
}
