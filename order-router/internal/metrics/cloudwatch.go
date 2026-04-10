package metrics

import (
	"context"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
)

const namespace = "Tradeflow/Router"

type Reporter struct {
	client *cloudwatch.Client
}

func NewReporter(ctx context.Context) (*Reporter, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-west-2"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return &Reporter{client: cloudwatch.NewFromConfig(cfg)}, nil
}

func (r *Reporter) RecordRoutedToPriority(ctx context.Context) {
	r.emit(ctx, "RoutedToPriority", 1)
}

func (r *Reporter) RecordRoutedToStandard(ctx context.Context) {
	r.emit(ctx, "RoutedToStandard", 1)
}

func (r *Reporter) RecordRoutingError(ctx context.Context) {
	r.emit(ctx, "RoutingErrors", 1)
}

func (r *Reporter) RecordCircuitBreakerOpen(ctx context.Context) {
	r.emit(ctx, "CircuitBreakerOpen", 1)
}

func (r *Reporter) emit(ctx context.Context, metricName string, value float64) {
	_, err := r.client.PutMetricData(ctx, &cloudwatch.PutMetricDataInput{
		Namespace: aws.String(namespace),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String(metricName),
				Value:      aws.Float64(value),
				Unit:       types.StandardUnitCount,
			},
		},
	})
	if err != nil {
		log.Printf("failed to emit metric %s: %v", metricName, err)
	}
}
