package adaptive

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/shanaya1981/tradeflow/order-router/internal/router"
	"github.com/sony/gobreaker"
)

const (
	//priorityQueueURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-priority"
	//standardQueueURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-standard"
	//costAlertURL     = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-cost-alerts"
	priorityQueueURL = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-priority"
	standardQueueURL = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-standard"
	costAlertURL     = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-cost-alerts"

	priorityThreshold       = 1000
	preMarketPriorityThresh = 500 // lower threshold during pre-market = more orders get priority
	preMarketWindowMinutes  = 5   // start pre-market mode 5 minutes before market open
)

// CostAlert mirrors what the dollar cost engine publishes
type CostAlert struct {
	AlertType       string  `json:"alert_type"`
	SlippagePerMin  float64 `json:"slippage_per_min"`
	TotalCostPerMin float64 `json:"total_cost_per_min"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	OrdersPerMin    int     `json:"orders_per_min"`
	Timestamp       int64   `json:"timestamp"`
	Message         string  `json:"message"`
}

type CircuitBreakerRouter struct {
	client          *sqs.Client
	breaker         *gobreaker.CircuitBreaker
	costTripped     bool
	costTrippedMu   sync.RWMutex
	preMarketActive bool
	preMarketMu     sync.RWMutex
	marketOpenHour  int // hour in UTC (9:30 EST = 13:30 UTC, so 13)
	marketOpenMin   int // minute (30)
}

func NewCircuitBreakerRouter(ctx context.Context) (*CircuitBreakerRouter, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// circuit breaker settings:
	// opens after 5 consecutive failures OR when cost alert received
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
			log.Printf("circuit breaker [%s] state changed: %s -> %s", name, from, to)
		},
	}

	cbr := &CircuitBreakerRouter{
		client:         sqs.NewFromConfig(cfg),
		breaker:        gobreaker.NewCircuitBreaker(settings),
		marketOpenHour: 13, // 9:30 AM EST = 13:30 UTC
		marketOpenMin:  30,
	}

	// start listening for cost alerts from dollar cost engine
	go cbr.listenForCostAlerts(ctx)

	// start predictive scaling scheduler
	go cbr.predictiveScaling(ctx)

	return cbr, nil
}

func (cb *CircuitBreakerRouter) Route(ctx context.Context, order router.Order) error {
	// check if cost-tripped — reject large orders to protect the firm
	cb.costTrippedMu.RLock()
	costTripped := cb.costTripped
	cb.costTrippedMu.RUnlock()

	if costTripped && order.Quantity > priorityThreshold {
		log.Printf("COST CIRCUIT OPEN: rejecting large order sender=%s qty=%d to protect against further slippage",
			order.SenderID, order.Quantity)
		return fmt.Errorf("cost circuit breaker open: order rejected to prevent further losses")
	}

	// determine priority threshold based on pre-market mode
	threshold := priorityThreshold
	cb.preMarketMu.RLock()
	if cb.preMarketActive {
		threshold = preMarketPriorityThresh
	}
	cb.preMarketMu.RUnlock()

	if order.Quantity > threshold {
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

// listenForCostAlerts polls the cost-alerts queue and trips the circuit breaker
// when the dollar cost engine detects costs exceeding thresholds
func (cb *CircuitBreakerRouter) listenForCostAlerts(ctx context.Context) {
	log.Println("cost alert listener started, polling tradeflow-cost-alerts")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		output, err := cb.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(costAlertURL),
			MaxNumberOfMessages: 5,
			WaitTimeSeconds:     5,
		})
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range output.Messages {
			var alert CostAlert
			if err := json.Unmarshal([]byte(*msg.Body), &alert); err != nil {
				continue
			}

			log.Printf("COST ALERT RECEIVED: %s | cost=$%.2f/min | latency=%.1fms",
				alert.AlertType, alert.TotalCostPerMin, alert.AvgLatencyMs)

			// trip the cost circuit breaker
			cb.costTrippedMu.Lock()
			cb.costTripped = true
			cb.costTrippedMu.Unlock()

			log.Printf("COST CIRCUIT BREAKER TRIPPED: rejecting large orders for 30 seconds")

			// auto-recover after 30 seconds
			go func() {
				time.Sleep(30 * time.Second)
				cb.costTrippedMu.Lock()
				cb.costTripped = false
				cb.costTrippedMu.Unlock()
				log.Printf("COST CIRCUIT BREAKER RECOVERED: accepting orders normally")
			}()

			_, _ = cb.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(costAlertURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
		}
	}
}

// predictiveScaling monitors the clock and activates pre-market mode
// before the known market open thundering herd at 9:30 AM EST
func (cb *CircuitBreakerRouter) predictiveScaling(ctx context.Context) {
	log.Printf("predictive scaling active: monitoring for market open at %02d:%02d UTC", cb.marketOpenHour, cb.marketOpenMin)

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().UTC()
			marketOpen := time.Date(now.Year(), now.Month(), now.Day(),
				cb.marketOpenHour, cb.marketOpenMin, 0, 0, time.UTC)

			minutesUntilOpen := marketOpen.Sub(now).Minutes()

			cb.preMarketMu.Lock()
			wasActive := cb.preMarketActive

			if minutesUntilOpen > 0 && minutesUntilOpen <= preMarketWindowMinutes {
				// we are in the pre-market window
				if !wasActive {
					log.Printf("PREDICTIVE SCALING: pre-market mode ACTIVATED (%.1f min until market open)", minutesUntilOpen)
					log.Printf("  priority threshold lowered: %d -> %d (more orders get priority lane)",
						priorityThreshold, preMarketPriorityThresh)
				}
				cb.preMarketActive = true
			} else if minutesUntilOpen <= 0 && minutesUntilOpen > -5 {
				// market is open, keep pre-market mode for 5 more minutes
				if !wasActive {
					log.Printf("PREDICTIVE SCALING: market open surge mode ACTIVE")
				}
				cb.preMarketActive = true
			} else {
				if wasActive {
					log.Printf("PREDICTIVE SCALING: pre-market mode DEACTIVATED (returning to normal thresholds)")
				}
				cb.preMarketActive = false
			}
			cb.preMarketMu.Unlock()
		}
	}
}

// State returns current circuit breaker state for CloudWatch reporting
func (cb *CircuitBreakerRouter) State() gobreaker.State {
	return cb.breaker.State()
}

// IsCostTripped returns whether the cost-based circuit breaker is active
func (cb *CircuitBreakerRouter) IsCostTripped() bool {
	cb.costTrippedMu.RLock()
	defer cb.costTrippedMu.RUnlock()
	return cb.costTripped
}

// IsPreMarketActive returns whether predictive scaling is active
func (cb *CircuitBreakerRouter) IsPreMarketActive() bool {
	cb.preMarketMu.RLock()
	defer cb.preMarketMu.RUnlock()
	return cb.preMarketActive
}
