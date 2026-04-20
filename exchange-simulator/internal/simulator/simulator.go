package simulator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	//priorityQueueURL  = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-priority"
	//standardQueueURL  = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-standard"
	//execReportURL     = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-exec-reports"
	priorityQueueURL  = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-priority"
	standardQueueURL  = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-standard"
	execReportURL     = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-exec-reports"
	priceDriftPerMs   = 0.00002 // realistic: $0.00002 per ms of delay
	maxSlippagePct    = 0.02    // cap slippage at 2% of order price
	rejectProbability = 0.05    // 5% of orders get rejected
	partialFillProb   = 0.15    // 15% get partial fills
	workerCount       = 10      // concurrent workers per queue
)

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

type Simulator struct {
	client *sqs.Client
}

func New(ctx context.Context) (*Simulator, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Simulator{client: sqs.NewFromConfig(cfg)}, nil
}

func (s *Simulator) Run(ctx context.Context) {
	log.Printf("exchange simulator started with %d workers per queue", workerCount)

	var wg sync.WaitGroup

	// launch workers for priority queue
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.pollQueue(ctx, priorityQueueURL, fmt.Sprintf("priority-%d", id))
		}(i)
	}

	// launch workers for standard queue
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.pollQueue(ctx, standardQueueURL, fmt.Sprintf("standard-%d", id))
		}(i)
	}

	wg.Wait()
}

func (s *Simulator) pollQueue(ctx context.Context, queueURL string, label string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		output, err := s.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(queueURL),
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     1,
		})
		if err != nil {
			log.Printf("[%s] receive error: %v", label, err)
			time.Sleep(500 * time.Millisecond)
			continue
		}

		for _, msg := range output.Messages {
			var order Order
			if err := json.Unmarshal([]byte(*msg.Body), &order); err != nil {
				log.Printf("[%s] unmarshal error: %v", label, err)
				continue
			}

			report := s.simulateFill(order)
			log.Printf("[%s] %s sender=%s qty=%d orderPx=%.2f fillPx=%.2f filled=%d latency=%.1fms",
				label, report.Status, report.SenderID, report.Quantity,
				report.OrderPrice, report.FillPrice, report.FilledQty, report.TransitLatencyMs)

			if err := s.publishReport(ctx, report); err != nil {
				log.Printf("[%s] failed to publish exec report: %v", label, err)
				continue
			}

			_, _ = s.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(queueURL),
				ReceiptHandle: msg.ReceiptHandle,
			})
		}
	}
}

func (s *Simulator) simulateFill(order Order) ExecutionReport {
	now := time.Now().UnixMilli()
	transitLatency := float64(now - order.Timestamp)
	if transitLatency < 0 {
		transitLatency = 1
	}

	report := ExecutionReport{
		SenderID:         order.SenderID,
		TargetID:         order.TargetID,
		Side:             order.Side,
		Quantity:         order.Quantity,
		OrderPrice:       order.Price,
		MsgType:          "8",
		OrderTimestamp:   order.Timestamp,
		FillTimestamp:    now,
		TransitLatencyMs: transitLatency,
	}

	roll := rand.Float64()

	if roll < rejectProbability {
		report.Status = "REJECTED"
		report.FillPrice = 0
		report.FilledQty = 0
		return report
	}

	// calculate slippage based on transit latency, capped at maxSlippagePct
	direction := 1.0
	if order.Side == "SELL" {
		direction = -1.0
	}
	slippage := transitLatency * priceDriftPerMs * direction
	// cap slippage at max percentage of order price
	maxSlippage := order.Price * maxSlippagePct
	if math.Abs(slippage) > maxSlippage {
		if slippage > 0 {
			slippage = maxSlippage
		} else {
			slippage = -maxSlippage
		}
	}
	// add small random noise
	noise := (rand.Float64() - 0.5) * 0.02
	report.FillPrice = math.Round((order.Price+slippage+noise)*100) / 100

	if roll < rejectProbability+partialFillProb {
		fillPct := 0.2 + rand.Float64()*0.6
		report.FilledQty = int(float64(order.Quantity) * fillPct)
		if report.FilledQty < 1 {
			report.FilledQty = 1
		}
		report.Status = "PARTIAL"
	} else {
		report.FilledQty = order.Quantity
		report.Status = "FILLED"
	}

	return report
}

func (s *Simulator) publishReport(ctx context.Context, report ExecutionReport) error {
	body, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("failed to marshal exec report: %w", err)
	}

	_, err = s.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(execReportURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("failed to send exec report: %w", err)
	}

	return nil
}
