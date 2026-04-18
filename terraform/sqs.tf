# SQS Queues
resource "aws_sqs_queue" "orders" {
  name                       = "tradeflow-orders"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 86400
  tags                       = { Project = var.project_name }
}

resource "aws_sqs_queue" "priority" {
  name                       = "tradeflow-priority"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 86400
  tags                       = { Project = var.project_name }
}

resource "aws_sqs_queue" "standard" {
  name                       = "tradeflow-standard"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 86400
  tags                       = { Project = var.project_name }
}

resource "aws_sqs_queue" "exec_reports" {
  name                       = "tradeflow-exec-reports"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 86400
  tags                       = { Project = var.project_name }
}

resource "aws_sqs_queue" "cost_alerts" {
  name                       = "tradeflow-cost-alerts"
  visibility_timeout_seconds = 30
  message_retention_seconds  = 86400
  tags                       = { Project = var.project_name }
}
