FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o dollar-cost-engine ./cmd/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/dollar-cost-engine .
CMD ["./dollar-cost-engine"]
