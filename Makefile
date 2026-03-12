.PHONY: run-contacts run-apps run-media test-services

run-contacts:
	@echo "Running contacts service on :18085"
	go run ./ohmf/services/contacts

run-apps:
	@echo "Running apps service on :18086"
	go run ./ohmf/services/apps

run-media:
	@echo "Running media service on :18087"
	go run ./ohmf/services/media

test-services:
	@echo "Running tests for services..."
	go test ./ohmf/services/... ./ohmf/pkg/observability -v
