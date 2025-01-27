FROM golang:1.23-alpine AS builder

WORKDIR /app
COPY go.* ./

RUN go mod download
RUN go mod tidy

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o bin/webhook

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/bin/webhook /usr/local/bin/
RUN chmod +x /usr/local/bin/webhook
CMD ["webhook"]