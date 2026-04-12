package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/shanaya1981/tradeflow/dollar-cost-engine/internal/engine"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	eng, err := engine.New(ctx)
	if err != nil {
		log.Fatalf("failed to create dollar cost engine: %v", err)
	}

	// graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down dollar cost engine")
		cancel()
	}()

	log.Println("dollar-cost-engine starting")
	eng.Run(ctx)
}
