package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/shanaya1981/tradeflow/exchange-simulator/internal/simulator"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sim, err := simulator.New(ctx)
	if err != nil {
		log.Fatalf("failed to create simulator: %v", err)
	}

	// graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down exchange simulator")
		cancel()
	}()

	log.Println("exchange-simulator starting")
	sim.Run(ctx)
}
