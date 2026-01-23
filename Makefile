.PHONY: build run test test-coverage test-race lint clean docker-build docker-push deploy helm-lint

IMAGE_NAME ?= fencemaster
IMAGE_TAG ?= latest
COVERAGE_DIR ?= coverage

build:
	go build -o bin/fencemaster ./cmd/webhook

run:
	go run ./cmd/webhook

# Run all tests
test:
	go test -v ./...

# Run tests with coverage report
test-coverage:
	@mkdir -p $(COVERAGE_DIR)
	go test -v -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./...
	go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html
	@echo "Coverage report generated at $(COVERAGE_DIR)/coverage.html"
	go tool cover -func=$(COVERAGE_DIR)/coverage.out

# Run tests with race detection
test-race:
	go test -v -race ./...

# Run tests in short mode (skip long-running tests)
test-short:
	go test -v -short ./...

# Run linter
lint:
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run ./...

# Run all checks (lint + test)
check: lint test

clean:
	rm -rf bin/
	rm -rf $(COVERAGE_DIR)/

docker-build:
	docker build -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-push:
	docker push $(IMAGE_NAME):$(IMAGE_TAG)

# Lint Helm chart
helm-lint:
	helm lint charts/fencemaster

# Template Helm chart (for debugging)
helm-template:
	helm template test charts/fencemaster

# CI targets
ci: lint test-coverage

# Local development
dev: build
	./bin/fencemaster --log-format=text --log-level=debug
