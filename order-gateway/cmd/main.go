package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/shanaya1981/tradeflow/order-gateway/internal/fix"
)

func main() {
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

	c.JSON(http.StatusAccepted, gin.H{
		"status": "received",
		"order":  order,
	})
}
