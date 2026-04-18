FROM golang:1.25-alpine AS builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /bin/sms-processor ./cmd/processor

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/sms-processor /bin/sms-processor
ENTRYPOINT ["/bin/sms-processor"]
