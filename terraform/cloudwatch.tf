resource "aws_cloudwatch_dashboard" "tradeflow" {
  dashboard_name = "tradeflow-dashboard"

  dashboard_body = jsonencode({
    widgets = [
      {
        type   = "metric"
        x      = 0
        y      = 0
        width  = 12
        height = 6
        properties = {
          title  = "Messages routed"
          view   = "timeSeries"
          stacked = false
          region = "us-west-2"
          metrics = [
            ["Tradeflow/Router", "RoutedToPriority"],
            ["Tradeflow/Router", "RoutedToStandard"],
            ["Tradeflow/Router", "RoutingErrors"]
          ]
          period = 60
          stat   = "Sum"
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 0
        width  = 12
        height = 6
        properties = {
          title  = "Circuit breaker state"
          view   = "timeSeries"
          stacked = false
          region = "us-west-2"
          metrics = [
            ["Tradeflow/Router", "CircuitBreakerOpen"]
          ]
          period = 60
          stat   = "Sum"
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 6
        width  = 24
        height = 6
        properties = {
          title  = "SQS queue depths"
          view   = "timeSeries"
          stacked = false
          region = "us-west-2"
          metrics = [
            ["AWS/SQS", "ApproximateNumberOfMessages", "QueueName", "tradeflow-orders"],
            ["AWS/SQS", "ApproximateNumberOfMessages", "QueueName", "tradeflow-priority"],
            ["AWS/SQS", "ApproximateNumberOfMessages", "QueueName", "tradeflow-standard"],
            ["AWS/SQS", "ApproximateNumberOfMessages", "QueueName", "tradeflow-cost-alerts"],
            ["AWS/SQS", "ApproximateNumberOfMessages", "QueueName", "tradeflow-exec-reports"]
          ]
          period = 60
          stat   = "Average"
        }
      },
      {
        type   = "metric"
        x      = 0
        y      = 12
        width  = 12
        height = 6
        properties = {
          title  = "Dollar cost per minute"
          view   = "timeSeries"
          stacked = false
          region = "us-west-2"
          metrics = [
            ["Tradeflow/CostEngine", "DollarLossPerMinute"]
          ]
          period = 60
          stat   = "Sum"
        }
      },
      {
        type   = "metric"
        x      = 12
        y      = 12
        width  = 12
        height = 6
        properties = {
          title  = "Routing errors"
          view   = "timeSeries"
          stacked = false
          region = "us-west-2"
          metrics = [
            ["Tradeflow/Router", "RoutingErrors"]
          ]
          period = 60
          stat   = "Sum"
        }
      }
    ]
  })
}