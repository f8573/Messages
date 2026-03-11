.PHONY: run build test sqlc

run:
	go run ./cmd/api

build:
	go build ./...

test:
	go test ./...

integration-test:
	OHMF_RUN_INTEGRATION=1 go test ./integration -v

sqlc:
	sqlc generate
