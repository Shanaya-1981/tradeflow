package main

import (
	"context"
	"log"
	"net/http"

	"github.com/Shanaya-1981/tradeflow/order-gateway/internal/fix"
	"github.com/Shanaya-1981/tradeflow/order-gateway/internal/queue"
	"github.com/gin-gonic/gin"
)

var sqsClient *queue.SQSClient

func main() {
	ctx := context.Background()

	var err error
	sqsClient, err = queue.NewSQSClient(ctx)
	if err != nil {
		log.Fatalf("failed to connect to SQS: %v", err)
	}
	log.Println("connected to SQS successfully")

	r := gin.Default()

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/order", handleOrder)

	log.Println("order-gateway running on :8080")
	r.Run(":8080")
}

func handleOrder(c *gin.Context) {
	var body struct {
		Message string `json:"message"`
	}

	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	order := fix.Parse(body.Message)
	log.Printf("parsed order: %+v", order)

	if err := sqsClient.SendOrder(context.Background(), order); err != nil {
		log.Printf("failed to send to SQS: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to queue order"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{
		"status": "queued",
		"order":  order,
	})
}
