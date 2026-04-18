package main

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/shanaya1981/tradeflow/order-router/internal/adaptive"
	"github.com/shanaya1981/tradeflow/order-router/internal/metrics"
	"github.com/shanaya1981/tradeflow/order-router/internal/queue"
)

const workerCount = 10

func main() {
	ctx := context.Background()

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

	log.Printf("order-router starting with %d workers", workerCount)

	var wg sync.WaitGroup

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			consumer, err := queue.NewConsumer(ctx)
			if err != nil {
				log.Fatalf("worker %d: failed to init consumer: %v", id, err)
			}

			log.Printf("worker %d: polling tradeflow-orders", id)

			for {
				orders, handles, err := consumer.Poll(ctx)
				if err != nil {
					log.Printf("worker %d: poll error: %v", id, err)
					time.Sleep(500 * time.Millisecond)
					continue
				}

				for i, order := range orders {
					err := cbRouter.Route(ctx, order)
					if err != nil {
						log.Printf("worker %d: route error for %s: %v", id, order.SenderID, err)
						reporter.RecordRoutingError(ctx)
						continue
					}

					if order.Quantity > 1000 {
						reporter.RecordRoutedToPriority(ctx)
					} else {
						reporter.RecordRoutedToStandard(ctx)
					}

					if err := consumer.Delete(ctx, handles[i]); err != nil {
						log.Printf("worker %d: delete error: %v", id, err)
					}
				}
			}
		}(i)
	}

	wg.Wait()
}
