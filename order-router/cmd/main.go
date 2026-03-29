package main

import (
	"context"
	"log"
	"time"

	"github.com/shanaya1981/tradeflow/order-router/internal/queue"
	"github.com/shanaya1981/tradeflow/order-router/internal/router"
)

func main() {
	ctx := context.Background()

	consumer, err := queue.NewConsumer(ctx)
	if err != nil {
		log.Fatalf("failed to init consumer: %v", err)
	}
	log.Println("connected to SQS successfully")

	r, err := router.New(ctx)
	if err != nil {
		log.Fatalf("failed to init router: %v", err)
	}
	log.Println("order-router polling from tradeflow-orders...")

	for {
		orders, handles, err := consumer.Poll(ctx)
		if err != nil {
			log.Printf("poll error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for i, order := range orders {
			if err := r.Route(ctx, order); err != nil {
				log.Printf("route error for order from %s: %v", order.SenderID, err)
				continue
			}

			if err := consumer.Delete(ctx, handles[i]); err != nil {
				log.Printf("delete error: %v", err)
			}
		}
	}
}
