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

type Engine struct {
	client *sqs.Client
	stats  *AggregateStats
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
	}, nil
}

// Run starts polling execution reports and printing cost analysis
func (e *Engine) Run(ctx context.Context) {
	log.Println("dollar cost engine started, polling execution reports queue")

	// print summary every 10 seconds
	go e.printSummary(ctx)

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

			log.Printf("COST | %s | sender=%s side=%s qty=%d | orderPx=%.2f fillPx=%.2f | slippage=$%.2f opportunity=$%.2f | TOTAL=$%.2f | latency=%.1fms",
				result.Status, result.SenderID, result.Side, result.Quantity,
				result.OrderPrice, result.FillPrice,
				result.SlippageCost, result.OpportunityCost, result.TotalCost,
				result.TransitLatencyMs)

			// delete from queue after processing
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
		// slippage cost = difference between what you wanted and what you got
		// for BUY: positive means you paid more (bad)
		// for SELL: positive means you received less (bad)
		priceDiff := report.FillPrice - report.OrderPrice
		if report.Side == "SELL" {
			priceDiff = report.OrderPrice - report.FillPrice
		}
		result.SlippageCost = math.Abs(priceDiff) * float64(report.FilledQty)
		result.OpportunityCost = 0
		result.TotalCost = result.SlippageCost

	case "PARTIAL":
		// slippage on what filled
		priceDiff := report.FillPrice - report.OrderPrice
		if report.Side == "SELL" {
			priceDiff = report.OrderPrice - report.FillPrice
		}
		result.SlippageCost = math.Abs(priceDiff) * float64(report.FilledQty)

		// opportunity cost on what didn't fill
		// estimated as the potential gain lost on unfilled shares
		// using 0.1% of order value per unfilled share as conservative estimate
		result.OpportunityCost = float64(result.UnfilledQty) * report.OrderPrice * 0.001

		result.TotalCost = result.SlippageCost + result.OpportunityCost

	case "REJECTED":
		// full opportunity cost — the entire trade didn't happen
		result.SlippageCost = 0
		result.OpportunityCost = float64(report.Quantity) * report.OrderPrice * 0.001
		result.TotalCost = result.OpportunityCost
	}

	// round to 2 decimal places
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

// GetStats returns current aggregate stats (for CloudWatch or API)
func (e *Engine) GetStats() AggregateStats {
	e.stats.mu.Lock()
	defer e.stats.mu.Unlock()
	return *e.stats
}
