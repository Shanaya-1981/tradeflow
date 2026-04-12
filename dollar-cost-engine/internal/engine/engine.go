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
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	execReportURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-exec-reports"
	costAlertURL  = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-cost-alerts"

	// cost thresholds — if exceeded, publish alert to trigger circuit breaker
	SlippageCostThreshold = 500.0  // per minute
	TotalCostThreshold    = 1000.0 // per minute
)

// ExecutionReport mirrors what the exchange simulator publishes
type ExecutionReport struct {
	SenderID         string  `json:"sender_id"`
	TargetID         string  `json:"target_id"`
	Side             string  `json:"side"`
	Quantity         int     `json:"quantity"`
	OrderPrice       float64 `json:"order_price"`
	FillPrice        float64 `json:"fill_price"`
	FilledQty        int     `json:"filled_qty"`
	Status           string  `json:"status"`
	MsgType          string  `json:"msg_type"`
	OrderTimestamp   int64   `json:"order_timestamp"`
	FillTimestamp    int64   `json:"fill_timestamp"`
	TransitLatencyMs float64 `json:"transit_latency_ms"`
}

// CostResult is the dollar cost analysis for a single order
type CostResult struct {
	SenderID         string  `json:"sender_id"`
	Side             string  `json:"side"`
	Quantity         int     `json:"quantity"`
	OrderPrice       float64 `json:"order_price"`
	FillPrice        float64 `json:"fill_price"`
	FilledQty        int     `json:"filled_qty"`
	UnfilledQty      int     `json:"unfilled_qty"`
	Status           string  `json:"status"`
	TransitLatencyMs float64 `json:"transit_latency_ms"`
	SlippageCost     float64 `json:"slippage_cost"`
	OpportunityCost  float64 `json:"opportunity_cost"`
	TotalCost        float64 `json:"total_cost"`
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
	mu                sync.Mutex
	TotalOrders       int     `json:"total_orders"`
	FilledOrders      int     `json:"filled_orders"`
	PartialOrders     int     `json:"partial_orders"`
	RejectedOrders    int     `json:"rejected_orders"`
	TotalSlippageCost float64 `json:"total_slippage_cost"`
	TotalOpportCost   float64 `json:"total_opportunity_cost"`
	TotalDollarCost   float64 `json:"total_dollar_cost"`
	AvgLatencyMs      float64 `json:"avg_latency_ms"`
	MaxLatencyMs      float64 `json:"max_latency_ms"`
	TotalLatencyMs    float64 `json:"-"`
}

// WindowStats tracks cost within the current time window
type WindowStats struct {
	mu             sync.Mutex
	SlippageCost   float64
	TotalCost      float64
	OrderCount     int
	TotalLatencyMs float64
	WindowStart    time.Time
}

type Engine struct {
	client *sqs.Client
	stats  *AggregateStats
	window *WindowStats
}

func New(ctx context.Context) (*Engine, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Engine{
		client: sqs.NewFromConfig(cfg),
		stats:  &AggregateStats{},
		window: &WindowStats{WindowStart: time.Now()},
	}, nil
}

// Run starts polling execution reports and printing cost analysis
func (e *Engine) Run(ctx context.Context) {
	log.Println("dollar cost engine started, polling execution reports queue")
	log.Printf("cost alert thresholds: slippage=$%.0f/min, total=$%.0f/min",
		SlippageCostThreshold, TotalCostThreshold)

	go e.printSummary(ctx)
	go e.monitorCostWindow(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		output, err := e.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(execReportURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     5,
		})
		if err != nil {
			log.Printf("receive error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range output.Messages {
			var report ExecutionReport
			if err := json.Unmarshal([]byte(*msg.Body), &report); err != nil {
				log.Printf("unmarshal error: %v", err)
				continue
			}

			result := e.calculateCost(report)
			e.updateStats(result)
			e.updateWindow(result)

			log.Printf("COST | %s | sender=%s side=%s qty=%d | orderPx=%.2f fillPx=%.2f | slippage=$%.2f opportunity=$%.2f | TOTAL=$%.2f | latency=%.1fms",
				result.Status, result.SenderID, result.Side, result.Quantity,
				result.OrderPrice, result.FillPrice,
				result.SlippageCost, result.OpportunityCost, result.TotalCost,
				result.TransitLatencyMs)

			_, _ = e.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(execReportURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
		}
	}
}

func (e *Engine) calculateCost(report ExecutionReport) CostResult {
	result := CostResult{
		SenderID:         report.SenderID,
		Side:             report.Side,
		Quantity:         report.Quantity,
		OrderPrice:       report.OrderPrice,
		FillPrice:        report.FillPrice,
		FilledQty:        report.FilledQty,
		UnfilledQty:      report.Quantity - report.FilledQty,
		Status:           report.Status,
		TransitLatencyMs: report.TransitLatencyMs,
	}

	switch report.Status {
	case "FILLED":
		priceDiff := report.FillPrice - report.OrderPrice
		if report.Side == "SELL" {
			priceDiff = report.OrderPrice - report.FillPrice
		}
		result.SlippageCost = math.Abs(priceDiff) * float64(report.FilledQty)
		result.OpportunityCost = 0
		result.TotalCost = result.SlippageCost

	case "PARTIAL":
		priceDiff := report.FillPrice - report.OrderPrice
		if report.Side == "SELL" {
			priceDiff = report.OrderPrice - report.FillPrice
		}
		result.SlippageCost = math.Abs(priceDiff) * float64(report.FilledQty)
		result.OpportunityCost = float64(result.UnfilledQty) * report.OrderPrice * 0.001
		result.TotalCost = result.SlippageCost + result.OpportunityCost

	case "REJECTED":
		result.SlippageCost = 0
		result.OpportunityCost = float64(report.Quantity) * report.OrderPrice * 0.001
		result.TotalCost = result.OpportunityCost
	}

	result.SlippageCost = math.Round(result.SlippageCost*100) / 100
	result.OpportunityCost = math.Round(result.OpportunityCost*100) / 100
	result.TotalCost = math.Round(result.TotalCost*100) / 100

	return result
}

func (e *Engine) updateStats(result CostResult) {
	e.stats.mu.Lock()
	defer e.stats.mu.Unlock()

	e.stats.TotalOrders++
	e.stats.TotalSlippageCost += result.SlippageCost
	e.stats.TotalOpportCost += result.OpportunityCost
	e.stats.TotalDollarCost += result.TotalCost
	e.stats.TotalLatencyMs += result.TransitLatencyMs

	if result.TransitLatencyMs > e.stats.MaxLatencyMs {
		e.stats.MaxLatencyMs = result.TransitLatencyMs
	}
	e.stats.AvgLatencyMs = e.stats.TotalLatencyMs / float64(e.stats.TotalOrders)

	switch result.Status {
	case "FILLED":
		e.stats.FilledOrders++
	case "PARTIAL":
		e.stats.PartialOrders++
	case "REJECTED":
		e.stats.RejectedOrders++
	}
}

func (e *Engine) updateWindow(result CostResult) {
	e.window.mu.Lock()
	defer e.window.mu.Unlock()

	e.window.SlippageCost += result.SlippageCost
	e.window.TotalCost += result.TotalCost
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
			totalCostPerMin := e.window.TotalCost / elapsed
			ordersPerMin := int(float64(e.window.OrderCount) / elapsed)
			avgLatency := 0.0
			if e.window.OrderCount > 0 {
				avgLatency = e.window.TotalLatencyMs / float64(e.window.OrderCount)
			}

			breached := false
			alertType := ""
			msg := ""

			if slippagePerMin > SlippageCostThreshold {
				breached = true
				alertType = "SLIPPAGE_THRESHOLD"
				msg = fmt.Sprintf("slippage cost $%.2f/min exceeds threshold $%.0f/min", slippagePerMin, SlippageCostThreshold)
			}
			if totalCostPerMin > TotalCostThreshold {
				breached = true
				alertType = "TOTAL_COST_THRESHOLD"
				msg = fmt.Sprintf("total cost $%.2f/min exceeds threshold $%.0f/min", totalCostPerMin, TotalCostThreshold)
			}

			if breached {
				alert := CostAlert{
					AlertType:       alertType,
					SlippagePerMin:  math.Round(slippagePerMin*100) / 100,
					TotalCostPerMin: math.Round(totalCostPerMin*100) / 100,
					AvgLatencyMs:    math.Round(avgLatency*100) / 100,
					OrdersPerMin:    ordersPerMin,
					Timestamp:       time.Now().UnixMilli(),
					Message:         msg,
				}

				log.Printf("COST ALERT: %s", msg)
				go e.publishAlert(ctx, alert)

				// reset window after alert
				e.window.SlippageCost = 0
				e.window.TotalCost = 0
				e.window.OrderCount = 0
				e.window.TotalLatencyMs = 0
				e.window.WindowStart = time.Now()
			}

			e.window.mu.Unlock()
		}
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
				log.Printf("  Orders: %d total | %d filled | %d partial | %d rejected",
					e.stats.TotalOrders, e.stats.FilledOrders, e.stats.PartialOrders, e.stats.RejectedOrders)
				log.Printf("  Slippage Cost:     $%.2f", e.stats.TotalSlippageCost)
				log.Printf("  Opportunity Cost:  $%.2f", e.stats.TotalOpportCost)
				log.Printf("  TOTAL DOLLAR COST: $%.2f", e.stats.TotalDollarCost)
				log.Printf("  Avg Latency: %.1fms | Max Latency: %.1fms",
					e.stats.AvgLatencyMs, e.stats.MaxLatencyMs)
				log.Printf("===========================")
			}
			e.stats.mu.Unlock()
		}
	}
}

// GetStats returns current aggregate stats
func (e *Engine) GetStats() AggregateStats {
	e.stats.mu.Lock()
	defer e.stats.mu.Unlock()
	return *e.stats
}
