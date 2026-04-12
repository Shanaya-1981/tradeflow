package adaptive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/shanaya1981/tradeflow/order-router/internal/router"
	"github.com/sony/gobreaker"
)

const (
	priorityQueueURL  = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-priority"
	standardQueueURL  = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-standard"
	priorityThreshold = 1000
)

type CircuitBreakerRouter struct {
	client  *sqs.Client
	breaker *gobreaker.CircuitBreaker
}

func NewCircuitBreakerRouter(ctx context.Context) (*CircuitBreakerRouter, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// configure gobreaker
	// opens after 5 consecutive failures
	// stays open for 30 seconds before moving to half-open
	// requires 2 successes in half-open to close again
	settings := gobreaker.Settings{
		Name:        "priority-queue-breaker",
		MaxRequests: 2,
		Interval:    0,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
		OnStateChange: func(name string, from, to gobreaker.State) {
			log.Printf("circuit breaker [%s] state changed: %s → %s", name, from, to)
		},
	}

	return &CircuitBreakerRouter{
		client:  sqs.NewFromConfig(cfg),
		breaker: gobreaker.NewCircuitBreaker(settings),
	}, nil
}

func (cb *CircuitBreakerRouter) Route(ctx context.Context, order router.Order) error {
	if order.Quantity > priorityThreshold {
		return cb.routeWithBreaker(ctx, order)
	}
	return cb.sendToQueue(ctx, standardQueueURL, order)
}

// routeWithBreaker tries priority queue through the circuit breaker
// if circuit is open it falls back to standard queue
func (cb *CircuitBreakerRouter) routeWithBreaker(ctx context.Context, order router.Order) error {
	_, err := cb.breaker.Execute(func() (interface{}, error) {
		return nil, cb.sendToQueue(ctx, priorityQueueURL, order)
	})

	if err != nil {
		if err == gobreaker.ErrOpenState || err == gobreaker.ErrTooManyRequests {
			// circuit is open — fall back to standard queue instead of dropping order
			log.Printf("circuit open: falling back to standard queue for sender=%s qty=%d",
				order.SenderID, order.Quantity)
			return cb.sendToQueue(ctx, standardQueueURL, order)
		}
		return fmt.Errorf("priority queue error: %w", err)
	}

	log.Printf("priority route: sender=%s qty=%d price=%.2f",
		order.SenderID, order.Quantity, order.Price)
	return nil
}

func (cb *CircuitBreakerRouter) sendToQueue(ctx context.Context, queueURL string, order router.Order) error {
	body, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	_, err = cb.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("failed to send to %s: %w", queueURL, err)
	}

	return nil
}

// State returns current circuit breaker state for CloudWatch reporting
func (cb *CircuitBreakerRouter) State() gobreaker.State {
	return cb.breaker.State()
}
