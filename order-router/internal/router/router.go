package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

const (
	priorityQueueURL  = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-priority"
	standardQueueURL  = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-standard"
	priorityThreshold = 1000
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

type Router struct {
	client *sqs.Client
}

func New(ctx context.Context) (*Router, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Router{client: sqs.NewFromConfig(cfg)}, nil
}

func (r *Router) Route(ctx context.Context, order Order) error {
	target := standardQueueURL
	if order.Quantity > priorityThreshold {
		target = priorityQueueURL
		log.Printf("priority route: sender=%s qty=%d price=%.2f",
			order.SenderID, order.Quantity, order.Price)
	} else {
		log.Printf("standard route: sender=%s qty=%d price=%.2f",
			order.SenderID, order.Quantity, order.Price)
	}

	body, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	_, err = r.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(target),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("failed to send to %s: %w", target, err)
	}

	return nil
}
