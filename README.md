# Tradeflow

Distributed FIX Protocol Engine with Real-Time Dollar Cost Analysis

## What is this

Tradeflow is a distributed system that routes trade orders at scale and calculates the exact financial cost of every delay in real time. When latency causes slippage, the system detects it, measures it in dollars, and automatically trips a circuit breaker to stop further losses.

Most distributed systems measure latency in milliseconds. This one measures it in dollars.

## Why we built it

In financial infrastructure, a 200ms delay on a 5,000 share order at $150.50 can cost over $1,000 in slippage. Multiply that across thousands of orders and the numbers add up fast. Existing tools tell you that latency went up. They do not tell you what that latency cost the firm.

tradeflow was built to close that gap. It takes the standard metrics (latency, throughput, error rate) and converts them into a single signal that actually drives decisions: dollar cost per minute.

This project was built as part of CS 6650 Scalable Distributed Systems at Northeastern University, Spring 2026.

## Architecture

Four independently running Go microservices connected through five AWS SQS queues.

```
FIX Client --> Order Gateway --> [tradeflow-orders] --> Order Router
                                                           |
                                            +--------------+--------------+
                                            |                             |
                                   [tradeflow-priority]          [tradeflow-standard]
                                            |                             |
                                            +-------------+---------------+
                                                          |
                                                  Exchange Simulator
                                                          |
                                                 [tradeflow-exec-reports]
                                                          |
                                                  Dollar Cost Engine
                                                          |
                                                 [tradeflow-cost-alerts]
                                                          |
                                                  Circuit Breaker (Router)
```

**Order Gateway** receives raw FIX protocol messages, parses them into structured orders, stamps a millisecond timestamp, and publishes to SQS. Built with Go and the Gin HTTP framework.

**Order Router** polls the orders queue and routes by financial impact. Orders above 1,000 shares go to the priority queue, everything else to standard. Includes a cost-aware circuit breaker (Sony gobreaker) that trips when the dollar cost engine detects excessive slippage. Also implements predictive scaling that lowers the priority threshold before market open (9:30 AM EST) to prepare for the daily volume spike.

**Exchange Simulator** polls both priority and standard queues and simulates realistic exchange behavior. It calculates fill prices based on transit latency (longer delay means more price drift), decides fill/partial/reject outcomes, and publishes FIX execution reports (message type 35=8) to the exec-reports queue.

**Dollar Cost Engine** consumes execution reports and calculates the financial impact of every order. Slippage cost is the price difference between order and fill multiplied by shares filled. Opportunity cost covers unfilled and rejected shares. It monitors a rolling cost window and publishes alerts to the cost-alerts queue when thresholds are exceeded.

## The Feedback Loop

This is the core of tradeflow. The dollar cost engine monitors slippage in real time. When cost exceeds $500 per minute, it publishes a cost alert. The circuit breaker in the router receives that alert and starts rejecting large orders for 30 seconds to prevent further damage.

Most circuit breakers trip on error counts or latency thresholds. This one trips on financial impact. The system does not care that p99 is 200ms. It cares that money is leaving the building.

## Tech Stack

- **Language:** Go 1.25
- **HTTP Framework:** Gin
- **Message Queue:** AWS SQS (5 queues)
- **Circuit Breaker:** Sony gobreaker
- **Infrastructure:** AWS ECS Fargate, ALB, VPC, CloudWatch
- **Infrastructure as Code:** Terraform
- **Load Testing:** Locust (Python)
- **Containerization:** Docker

## Project Structure

```
tradeflow/
    order-gateway/          # FIX parser and HTTP endpoint
        cmd/main.go
        internal/fix/parser.go
        internal/queue/sqs.go
        Dockerfile
    order-router/           # Priority routing and circuit breaker
        cmd/main.go
        internal/router/router.go
        internal/adaptive/circuit_breaker.go
        internal/metrics/cloudwatch.go
        internal/queue/sqs.go
        Dockerfile
    exchange-simulator/     # Simulated exchange with slippage
        cmd/main.go
        internal/simulator/simulator.go
        Dockerfile
    dollar-cost-engine/     # Cost calculation and alerting
        cmd/main.go
        internal/engine/engine.go
        Dockerfile
    terraform/              # Full AWS infrastructure
        provider.tf
        variables.tf
        network.tf
        sqs.tf
        iam.tf
        ecr.tf
        alb.tf
        ecs.tf
        outputs.tf
    Load-Test/
        locustfile.py
```

## Running Locally

Prerequisites: Go 1.23+, AWS CLI configured, Docker

**1. Create the SQS queues**

```bash
aws sqs create-queue --queue-name tradeflow-orders --region us-west-2
aws sqs create-queue --queue-name tradeflow-priority --region us-west-2
aws sqs create-queue --queue-name tradeflow-standard --region us-west-2
aws sqs create-queue --queue-name tradeflow-exec-reports --region us-west-2
aws sqs create-queue --queue-name tradeflow-cost-alerts --region us-west-2
```

Note: Update the SQS queue URLs in each service's source code to match your AWS account ID.

**2. Install dependencies**

```bash
cd order-gateway && go mod tidy
cd ../order-router && go mod tidy
cd ../exchange-simulator && go mod tidy
cd ../dollar-cost-engine && go mod tidy
```

**3. Start all four services (each in a separate terminal)**

```bash
cd order-gateway && go run cmd/main.go
cd order-router && go run cmd/main.go
cd exchange-simulator && go run cmd/main.go
cd dollar-cost-engine && go run cmd/main.go
```

**4. Send a test order**

```bash
curl -X POST http://localhost:8080/order \
  -H "Content-Type: application/json" \
  -d '{"message": "8=FIX.4.2|35=D|49=CLIENT|56=BROKER|54=1|38=5000|44=150.50"}'
```

You should see the order flow through all four terminals with a dollar cost calculation at the end.

## Deploying to AWS

**1. Run Terraform**

```bash
cd terraform
terraform init
terraform apply
```

This creates the full infrastructure: VPC, subnets, ALB, ECS cluster, ECR repos, SQS queues, CloudWatch log groups, and four Fargate services.

**2. Build and push Docker images**

```bash
aws ecr get-login-password --region us-west-2 | docker login --username AWS --password-stdin <account-id>.dkr.ecr.us-west-2.amazonaws.com

cd order-gateway
docker build --platform linux/amd64 -t <account-id>.dkr.ecr.us-west-2.amazonaws.com/tradeflow-gateway:latest .
docker push <account-id>.dkr.ecr.us-west-2.amazonaws.com/tradeflow-gateway:latest
```

Repeat for order-router, exchange-simulator, and dollar-cost-engine with their respective ECR repo names.

**3. Force redeployment**

```bash
aws ecs update-service --cluster tradeflow-cluster --service tradeflow-gateway --force-new-deployment --region us-west-2
```

Repeat for the other three services.

## FIX Protocol Reference

FIX (Financial Information eXchange) is the standard messaging protocol used across the financial industry. tradeflow supports the following FIX tags:

| Tag | Field | Example |
|-----|-------|---------|
| 8 | FIX Version | FIX.4.2 |
| 35 | Message Type | D (New Order), 8 (Execution Report) |
| 49 | Sender ID | CLIENT |
| 56 | Target ID | BROKER |
| 54 | Side | 1 (Buy), 2 (Sell) |
| 38 | Quantity | 5000 |
| 44 | Price | 150.50 |

## Dollar Cost Formula

```
slippage_cost = |fill_price - order_price| x filled_quantity
opportunity_cost = unfilled_quantity x order_price x 0.001
total_cost = slippage_cost + opportunity_cost
```

Fill price is determined by transit latency through the system. The exchange simulator applies a price drift of $0.0005 per millisecond of delay.

## Configuration

Key thresholds that can be adjusted in the source code:

| Parameter | Default | Location |
|-----------|---------|----------|
| Priority routing threshold | 1,000 shares | order-router/internal/router/router.go |
| Pre-market threshold | 500 shares | order-router/internal/adaptive/circuit_breaker.go |
| Slippage alert threshold | $500/min | dollar-cost-engine/internal/engine/engine.go |
| Total cost alert threshold | $1,000/min | dollar-cost-engine/internal/engine/engine.go |
| Circuit breaker recovery | 30 seconds | order-router/internal/adaptive/circuit_breaker.go |
| Price drift per ms | $0.0005 | exchange-simulator/internal/simulator/simulator.go |

## Extending tradeflow

The architecture is designed so that the exchange simulator can be replaced with any backend that produces execution reports in the same JSON format. If you want to connect to a real paper trading API (like Alpaca), replace the simulator service while keeping the cost engine, circuit breaker, and router unchanged.

## Team

**Supriya Tiwari** (California): Order Gateway, FIX Parser, Exchange Simulator, Dollar Cost Engine, Terraform Infrastructure

**Navaneeth Maruthi** (Boston): Order Router, Circuit Breaker, CloudWatch Metrics Reporter, Locust Load Tests

## License

MIT