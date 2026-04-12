package queue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/shanaya1981/tradeflow/order-gateway/internal/fix"
)

// const queueURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-orders"
const queueURL = "https://sqs.us-west-2.amazonaws.com/685016798289/tradeflow-orders"

// type SQSClient struct {
// 	client *sqs.Client
// }

// func NewSQSClient(ctx context.Context) (*SQSClient, error) {
// 	cfg, err := config.LoadDefaultConfig(ctx)
// 	if err != nil {
// 		return nil, fmt.Errorf("failed to load AWS config: %w", err)
// 	}

// 	return &SQSClient{
// 		client: sqs.NewFromConfig(cfg),
// 	}, nil
// }

// func (s *SQSClient) SendOrder(ctx context.Context, order fix.Order) error {
// 	body, err := json.Marshal(order)
// 	if err != nil {
// 		return fmt.Errorf("failed to marshal order: %w", err)
// 	}

// 	_, err = s.client.SendMessage(ctx, &sqs.SendMessageInput{
// 		QueueUrl:    aws.String(queueURL),
// 		MessageBody: aws.String(string(body)),
// 	})
// 	if err != nil {
// 		return fmt.Errorf("failed to send message to SQS: %w", err)
// 	}

// 	return nil
// }

type SQSClient struct {
	client *sqs.Client
}

func NewSQSClient(ctx context.Context) (*SQSClient, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &SQSClient{
		client: sqs.NewFromConfig(cfg),
	}, nil
}

func (s *SQSClient) SendOrder(ctx context.Context, order fix.Order) error {
	body, err := json.Marshal(order)
	if err != nil {
		return fmt.Errorf("failed to marshal order: %w", err)
	}

	_, err = s.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	})
	if err != nil {
		return fmt.Errorf("failed to send message to SQS: %w", err)
	}

	return nil
}
