package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/shanaya1981/tradeflow/order-router/internal/router"
)

// const inputQueueURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-orders"
const inputQueueURL = "https://sqs.us-west-2.amazonaws.com/650685162309/tradeflow-orders"

type Consumer struct {
	client *sqs.Client
}

func NewConsumer(ctx context.Context) (*Consumer, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Consumer{client: sqs.NewFromConfig(cfg)}, nil
}

func (c *Consumer) Poll(ctx context.Context) ([]router.Order, []string, error) {
	out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(inputQueueURL),
		MaxNumberOfMessages: int32(10),
		WaitTimeSeconds:     int32(1),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to poll SQS: %w", err)
	}

	var orders []router.Order
	var handles []string

	for _, msg := range out.Messages {
		var o router.Order
		if err := json.Unmarshal([]byte(*msg.Body), &o); err != nil {
			log.Printf("failed to unmarshal message: %v", err)
			continue
		}
		orders = append(orders, o)
		handles = append(handles, *msg.ReceiptHandle)
	}

	return orders, handles, nil
}

func (c *Consumer) Delete(ctx context.Context, handle string) error {
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(inputQueueURL),
		ReceiptHandle: aws.String(handle),
	})
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}
	return nil
}
