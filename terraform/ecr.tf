# ECR Repositories
resource "aws_ecr_repository" "gateway" {
  name         = "${var.project_name}-gateway"
  force_delete = true
}

resource "aws_ecr_repository" "router" {
  name         = "${var.project_name}-router"
  force_delete = true
}

resource "aws_ecr_repository" "simulator" {
  name         = "${var.project_name}-simulator"
  force_delete = true
}

resource "aws_ecr_repository" "cost_engine" {
  name         = "${var.project_name}-cost-engine"
  force_delete = true
}

# CloudWatch Log Groups
resource "aws_cloudwatch_log_group" "gateway" {
  name              = "/ecs/${var.project_name}-gateway"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "router" {
  name              = "/ecs/${var.project_name}-router"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "simulator" {
  name              = "/ecs/${var.project_name}-simulator"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_group" "cost_engine" {
  name              = "/ecs/${var.project_name}-cost-engine"
  retention_in_days = 7
}
