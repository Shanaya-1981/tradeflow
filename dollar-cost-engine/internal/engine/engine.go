package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	//priorityQueueURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-priority"
	//standardQueueURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-standard"
	//costAlertURL     = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-cost-alerts"
	priorityQueueURL = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-priority"
	standardQueueURL = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-standard"
	costAlertURL     = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-cost-alerts"

	// price drift model: how much the market moves per ms of delay
	// at 100ms delay on a $200 stock: $200 * 0.00001 * 100 = $0.20 slippage per share
	priceDriftPerMs = 0.00001

	// cost alert thresholds
	SlippageCostThreshold = 500.0  // per minute
	TotalCostThreshold    = 1000.0 // per minute

	workerCount = 10 // concurrent workers per queue
)

// Order mirrors the struct from gateway and router
type Order struct {
	SenderID  string  `json:"sender_id"`
	TargetID  string  `json:"target_id"`
	Side      string  `json:"side"`
	Quantity  int     `json:"quantity"`
	Price     float64 `json:"price"`
	MsgType   string  `json:"msg_type"`
	Raw       string  `json:"raw"`
	Timestamp int64   `json:"timestamp"`
}

// CostResult is the dollar cost analysis for a single order
type CostResult struct {
	SenderID         string  `json:"sender_id"`
	Side             string  `json:"side"`
	Quantity         int     `json:"quantity"`
	Price            float64 `json:"price"`
	EstSlippage      float64 `json:"est_slippage_per_share"`
	SlippageCost     float64 `json:"slippage_cost"`
	TransitLatencyMs float64 `json:"transit_latency_ms"`
	QueueType        string  `json:"queue_type"`
}

// CostAlert is published to SQS to trigger the circuit breaker
type CostAlert struct {
	AlertType       string  `json:"alert_type"`
	SlippagePerMin  float64 `json:"slippage_per_min"`
	TotalCostPerMin float64 `json:"total_cost_per_min"`
	AvgLatencyMs    float64 `json:"avg_latency_ms"`
	OrdersPerMin    int     `json:"orders_per_min"`
	Timestamp       int64   `json:"timestamp"`
	Message         string  `json:"message"`
}

// AggregateStats tracks running totals
type AggregateStats struct {
	mu             sync.Mutex
	TotalOrders    int     `json:"total_orders"`
	PriorityOrders int     `json:"priority_orders"`
	StandardOrders int     `json:"standard_orders"`
	TotalSlippage  float64 `json:"total_slippage_cost"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	MaxLatencyMs   float64 `json:"max_latency_ms"`
	TotalLatencyMs float64 `json:"-"`
}

// WindowStats tracks cost within the current time window for alerts
type WindowStats struct {
	mu             sync.Mutex
	SlippageCost   float64
	OrderCount     int
	TotalLatencyMs float64
	WindowStart    time.Time
}

// Engine — single definition, includes cwClient for CloudWatch emission
type Engine struct {
	client   *sqs.Client
	cwClient *cloudwatch.Client // emits metrics to Tradeflow/CostEngine namespace
	stats    *AggregateStats
	window   *WindowStats
}

func New(ctx context.Context) (*Engine, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Engine{
		client:   sqs.NewFromConfig(cfg),
		cwClient: cloudwatch.NewFromConfig(cfg),
		stats:    &AggregateStats{},
		window:   &WindowStats{WindowStart: time.Now()},
	}, nil
}

func (e *Engine) Run(ctx context.Context) {
	log.Println("dollar cost engine started, polling priority and standard queues")
	log.Printf("cost model: $%.5f drift per ms per share", priceDriftPerMs)
	log.Printf("alert thresholds: slippage=$%.0f/min, total=$%.0f/min",
		SlippageCostThreshold, TotalCostThreshold)

	go e.printSummary(ctx)
	go e.monitorCostWindow(ctx)

	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			e.pollQueue(ctx, priorityQueueURL, "priority", id)
		}(i)
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			e.pollQueue(ctx, standardQueueURL, "standard", id)
		}(i)
	}

	wg.Wait()
}

func (e *Engine) pollQueue(ctx context.Context, queueURL string, queueType string, workerID int) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		output, err := e.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     1,
		})
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, msg := range output.Messages {
			var order Order
			if err := json.Unmarshal([]byte(*msg.Body), &order); err != nil {
				continue
			}

			result := e.calculateCost(order, queueType)
			e.updateStats(result)
			e.updateWindow(result)

			log.Printf("COST | %s | sender=%s side=%s qty=%d price=%.2f | slippage=$%.2f (est $%.4f/share) | latency=%.0fms",
				result.QueueType, result.SenderID, result.Side, result.Quantity,
				result.Price, result.SlippageCost, result.EstSlippage, result.TransitLatencyMs)

			_, _ = e.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
		}
	}
}

func (e *Engine) calculateCost(order Order, queueType string) CostResult {
	now := time.Now().UnixMilli()
	transitLatency := float64(now - order.Timestamp)
	if transitLatency < 0 {
		transitLatency = 1
	}

	estSlippage := order.Price * priceDriftPerMs * transitLatency
	slippageCost := math.Round(estSlippage*float64(order.Quantity)*100) / 100
	estSlippage = math.Round(estSlippage*10000) / 10000

	return CostResult{
		SenderID:         order.SenderID,
		Side:             order.Side,
		Quantity:         order.Quantity,
		Price:            order.Price,
		EstSlippage:      estSlippage,
		SlippageCost:     slippageCost,
		TransitLatencyMs: transitLatency,
		QueueType:        queueType,
	}
}

func (e *Engine) updateStats(result CostResult) {
	e.stats.mu.Lock()
	defer e.stats.mu.Unlock()

	e.stats.TotalOrders++
	e.stats.TotalSlippage += result.SlippageCost
	e.stats.TotalLatencyMs += result.TransitLatencyMs

	if result.TransitLatencyMs > e.stats.MaxLatencyMs {
		e.stats.MaxLatencyMs = result.TransitLatencyMs
	}
	e.stats.AvgLatencyMs = e.stats.TotalLatencyMs / float64(e.stats.TotalOrders)

	if result.QueueType == "priority" {
		e.stats.PriorityOrders++
	} else {
		e.stats.StandardOrders++
	}
}

func (e *Engine) updateWindow(result CostResult) {
	e.window.mu.Lock()
	defer e.window.mu.Unlock()

	e.window.SlippageCost += result.SlippageCost
	e.window.OrderCount++
	e.window.TotalLatencyMs += result.TransitLatencyMs
}

func (e *Engine) monitorCostWindow(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.window.mu.Lock()
			elapsed := time.Since(e.window.WindowStart).Minutes()
			if elapsed < 0.1 {
				elapsed = 0.1
			}

			slippagePerMin := e.window.SlippageCost / elapsed
			ordersPerMin := int(float64(e.window.OrderCount) / elapsed)
			avgLatency := 0.0
			if e.window.OrderCount > 0 {
				avgLatency = e.window.TotalLatencyMs / float64(e.window.OrderCount)
			}

			// always emit to CloudWatch every 15 seconds regardless of threshold
			// this ensures the dashboard always has data, not just during high load
			go e.emitCostMetric(ctx, slippagePerMin, avgLatency)

			if slippagePerMin > SlippageCostThreshold {
				alert := CostAlert{
					AlertType:       "SLIPPAGE_THRESHOLD",
					SlippagePerMin:  math.Round(slippagePerMin*100) / 100,
					TotalCostPerMin: math.Round(slippagePerMin*100) / 100,
					AvgLatencyMs:    math.Round(avgLatency*100) / 100,
					OrdersPerMin:    ordersPerMin,
					Timestamp:       time.Now().UnixMilli(),
					Message: fmt.Sprintf("slippage cost $%.2f/min exceeds threshold $%.0f/min (%d orders/min, avg latency %.0fms)",
						slippagePerMin, SlippageCostThreshold, ordersPerMin, avgLatency),
				}
				log.Printf("COST ALERT: %s", alert.Message)
				go e.publishAlert(ctx, alert)

				e.window.SlippageCost = 0
				e.window.OrderCount = 0
				e.window.TotalLatencyMs = 0
				e.window.WindowStart = time.Now()
			}

			e.window.mu.Unlock()
		}
	}
}

func (e *Engine) emitCostMetric(ctx context.Context, dollarLossPerMin float64, avgLatency float64) {
	_, err := e.cwClient.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String("Tradeflow/CostEngine"),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String("DollarLossPerMinute"),
				Value:      aws.Float64(dollarLossPerMin),
				Unit:       types.StandardUnitNone,
			},
			{
				MetricName: aws.String("AvgLatencyMs"),
				Value:      aws.Float64(avgLatency),
				Unit:       types.StandardUnitMilliseconds,
			},
		},
	})
	if err != nil {
		log.Printf("failed to emit cost metric: %v", err)
	}
}

func (e *Engine) publishAlert(ctx context.Context, alert CostAlert) {
	body, err := json.Marshal(alert)
	if err != nil {
		log.Printf("failed to marshal cost alert: %v", err)
		return
	}

	_, err = e.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(costAlertURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		log.Printf("failed to publish cost alert: %v", err)
		return
	}
	log.Printf("cost alert published to circuit breaker")
}

func (e *Engine) printSummary(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.stats.mu.Lock()
			if e.stats.TotalOrders > 0 {
				log.Printf("=== DOLLAR COST SUMMARY ===")
				log.Printf("  Orders: %d total | %d priority | %d standard",
					e.stats.TotalOrders, e.stats.PriorityOrders, e.stats.StandardOrders)
				log.Printf("  Total Slippage Cost: $%.2f", e.stats.TotalSlippage)
				log.Printf("  Avg Latency: %.1fms | Max Latency: %.1fms",
					e.stats.AvgLatencyMs, e.stats.MaxLatencyMs)
				log.Printf("  Cost per Order: $%.2f", e.stats.TotalSlippage/float64(e.stats.TotalOrders))
				log.Printf("===========================")
			}
			e.stats.mu.Unlock()
		}
	}
}

func (e *Engine) GetStats() AggregateStats {
	e.stats.mu.Lock()
	defer e.stats.mu.Unlock()
	return *e.stats
}
