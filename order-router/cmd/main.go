package main

import (
	"context"
	"log"
	"time"

	"github.com/shanaya1981/tradeflow/order-router/internal/adaptive"
	"github.com/shanaya1981/tradeflow/order-router/internal/metrics"
	"github.com/shanaya1981/tradeflow/order-router/internal/queue"
)

func main() {
	ctx := context.Background()

	consumer, err := queue.NewConsumer(ctx)
	if err != nil {
		log.Fatalf("failed to init consumer: %v", err)
	}
	log.Println("connected to SQS successfully")

	cbRouter, err := adaptive.NewCircuitBreakerRouter(ctx)
	if err != nil {
		log.Fatalf("failed to init circuit breaker router: %v", err)
	}
	log.Println("circuit breaker router ready")

	reporter, err := metrics.NewReporter(ctx)
	if err != nil {
		log.Fatalf("failed to init metrics reporter: %v", err)
	}
	log.Println("cloudwatch reporter ready")

	log.Println("order-router polling from tradeflow-orders...")

	for {
		orders, handles, err := consumer.Poll(ctx)
		if err != nil {
			log.Printf("poll error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for i, order := range orders {
			err := cbRouter.Route(ctx, order)
			if err != nil {
				log.Printf("route error for order from %s: %v", order.SenderID, err)
				reporter.RecordRoutingError(ctx)
				continue
			}

			if order.Quantity > 1000 {
				reporter.RecordRoutedToPriority(ctx)
			} else {
				reporter.RecordRoutedToStandard(ctx)
			}

			if err := consumer.Delete(ctx, handles[i]); err != nil {
				log.Printf("delete error: %v", err)
			}
		}
	}
}
