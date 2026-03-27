package fix

import (
	"strconv"
	"strings"
)

type Order struct {
	SenderID string  `json:"sender_id"`
	TargetID string  `json:"target_id"`
	Side     string  `json:"side"`
	Quantity int     `json:"quantity"`
	Price    float64 `json:"price"`
	MsgType  string  `json:"msg_type"`
	Raw      string  `json:"raw"`
}

func Parse(message string) Order {
	fields := strings.Split(message, "|")
	order := Order{Raw: message}

	for _, field := range fields {
		parts := strings.SplitN(field, "=", 2)
		if len(parts) != 2 {
			continue
		}
		tag, value := parts[0], parts[1]

		switch tag {
		case "35":
			order.MsgType = value
		case "49":
			order.SenderID = value
		case "56":
			order.TargetID = value
		case "54":
			if value == "1" {
				order.Side = "BUY"
			} else {
				order.Side = "SELL"
			}
		case "38":
			order.Quantity, _ = strconv.Atoi(value)
		case "44":
			order.Price, _ = strconv.ParseFloat(value, 64)
		}
	}
	return order
}
