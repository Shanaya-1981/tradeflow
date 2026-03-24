# tradeflow

A distributed FIX Protocol Engine with real-time dollar cost analysis.

Measures the exact financial impact of latency, failures, and sequencing violations
in high-throughput order routing and responds automatically through circuit breaking,
priority routing, and predictive scaling.

## Services

- `order-gateway` — FIX message ingestion and parsing
- `order-router` — Priority-based message routing (coming soon)

## Infrastructure

AWS ECS Fargate, SQS, DynamoDB, CloudWatch — fully managed via Terraform.

## Getting Started

_Setup instructions coming as services are implemented._
