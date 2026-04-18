output "alb_dns" {
  value       = aws_lb.gateway.dns_name
  description = "Gateway ALB DNS — send orders here"
}

output "ecr_gateway_url" {
  value = aws_ecr_repository.gateway.repository_url
}

output "ecr_router_url" {
  value = aws_ecr_repository.router.repository_url
}

output "ecr_simulator_url" {
  value = aws_ecr_repository.simulator.repository_url
}

output "ecr_cost_engine_url" {
  value = aws_ecr_repository.cost_engine.repository_url
}

output "sqs_orders_url" {
  value = aws_sqs_queue.orders.url
}

output "sqs_priority_url" {
  value = aws_sqs_queue.priority.url
}

output "sqs_standard_url" {
  value = aws_sqs_queue.standard.url
}

output "sqs_exec_reports_url" {
  value = aws_sqs_queue.exec_reports.url
}

output "sqs_cost_alerts_url" {
  value = aws_sqs_queue.cost_alerts.url
}
