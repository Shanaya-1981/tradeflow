# ECS Cluster
resource "aws_ecs_cluster" "main" {
  name = "${var.project_name}-cluster"
}

# ============================================
# ORDER GATEWAY — receives HTTP, needs ALB
# ============================================
resource "aws_ecs_task_definition" "gateway" {
  family                   = "${var.project_name}-gateway"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = data.aws_iam_role.lab_role.arn
  task_role_arn            = data.aws_iam_role.lab_role.arn

  container_definitions = jsonencode([{
    name      = "gateway"
    image     = "${aws_ecr_repository.gateway.repository_url}:latest"
    essential = true
    portMappings = [{ containerPort = 8080, protocol = "tcp" }]
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.gateway.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "gateway"
      }
    }
  }])
}

resource "aws_ecs_service" "gateway" {
  name            = "${var.project_name}-gateway"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.gateway.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = [aws_subnet.public_a.id, aws_subnet.public_b.id]
    security_groups  = [aws_security_group.gateway.id]
    assign_public_ip = true
  }

  load_balancer {
    target_group_arn = aws_lb_target_group.gateway.arn
    container_name   = "gateway"
    container_port   = 8080
  }
}

# ============================================
# ORDER ROUTER — polls SQS, no HTTP traffic
# ============================================
resource "aws_ecs_task_definition" "router" {
  family                   = "${var.project_name}-router"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = data.aws_iam_role.lab_role.arn
  task_role_arn            = data.aws_iam_role.lab_role.arn

  container_definitions = jsonencode([{
    name      = "router"
    image     = "${aws_ecr_repository.router.repository_url}:latest"
    essential = true
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.router.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "router"
      }
    }
  }])
}

resource "aws_ecs_service" "router" {
  name            = "${var.project_name}-router"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.router.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = [aws_subnet.public_a.id, aws_subnet.public_b.id]
    security_groups  = [aws_security_group.internal.id]
    assign_public_ip = true
  }
}

# ============================================
# EXCHANGE SIMULATOR — polls SQS, no HTTP
# ============================================
resource "aws_ecs_task_definition" "simulator" {
  family                   = "${var.project_name}-simulator"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = data.aws_iam_role.lab_role.arn
  task_role_arn            = data.aws_iam_role.lab_role.arn

  container_definitions = jsonencode([{
    name      = "simulator"
    image     = "${aws_ecr_repository.simulator.repository_url}:latest"
    essential = true
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.simulator.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "simulator"
      }
    }
  }])
}

resource "aws_ecs_service" "simulator" {
  name            = "${var.project_name}-simulator"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.simulator.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = [aws_subnet.public_a.id, aws_subnet.public_b.id]
    security_groups  = [aws_security_group.internal.id]
    assign_public_ip = true
  }
}

# ============================================
# DOLLAR COST ENGINE — polls SQS, no HTTP
# ============================================
resource "aws_ecs_task_definition" "cost_engine" {
  family                   = "${var.project_name}-cost-engine"
  network_mode             = "awsvpc"
  requires_compatibilities = ["FARGATE"]
  cpu                      = "256"
  memory                   = "512"
  execution_role_arn       = data.aws_iam_role.lab_role.arn
  task_role_arn            = data.aws_iam_role.lab_role.arn

  container_definitions = jsonencode([{
    name      = "cost-engine"
    image     = "${aws_ecr_repository.cost_engine.repository_url}:latest"
    essential = true
    logConfiguration = {
      logDriver = "awslogs"
      options = {
        "awslogs-group"         = aws_cloudwatch_log_group.cost_engine.name
        "awslogs-region"        = var.region
        "awslogs-stream-prefix" = "cost-engine"
      }
    }
  }])
}

resource "aws_ecs_service" "cost_engine" {
  name            = "${var.project_name}-cost-engine"
  cluster         = aws_ecs_cluster.main.id
  task_definition = aws_ecs_task_definition.cost_engine.arn
  desired_count   = 1
  launch_type     = "FARGATE"

  network_configuration {
    subnets          = [aws_subnet.public_a.id, aws_subnet.public_b.id]
    security_groups  = [aws_security_group.internal.id]
    assign_public_ip = true
  }
}
