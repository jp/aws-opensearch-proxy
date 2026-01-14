.PHONY: help build run test docker-build docker-run clean deps version

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_DATE := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -w -s -X main.Version=$(VERSION) -X main.BuildDate=$(BUILD_DATE) -X main.GitCommit=$(GIT_COMMIT)

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

deps: ## Download Go dependencies
	go mod download
	go mod tidy

build: deps ## Build the binary with version information
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o aws-opensearch-proxy .

version: ## Show version information
	@echo "Version:    $(VERSION)"
	@echo "Build Date: $(BUILD_DATE)"
	@echo "Git Commit: $(GIT_COMMIT)"

run: ## Run locally (requires OPENSEARCH_URL env var)
	go run main.go

test: ## Run tests
	go test -v -race ./...

docker-build: ## Build Docker image with version information
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg BUILD_DATE=$(BUILD_DATE) \
		--build-arg VCS_REF=$(GIT_COMMIT) \
		-t aws-opensearch-proxy:latest \
		-t aws-opensearch-proxy:$(VERSION) \
		.

docker-run: ## Run Docker container (requires OPENSEARCH_URL env var)
	docker run --rm -it \
		-p 8080:8080 \
		-e OPENSEARCH_URL=${OPENSEARCH_URL} \
		-e AWS_REGION=${AWS_REGION} \
		-e AWS_ACCESS_KEY_ID=${AWS_ACCESS_KEY_ID} \
		-e AWS_SECRET_ACCESS_KEY=${AWS_SECRET_ACCESS_KEY} \
		-e AWS_SESSION_TOKEN=${AWS_SESSION_TOKEN} \
		aws-opensearch-proxy:latest

k8s-apply: ## Apply Kubernetes manifests
	kubectl apply -f namespace.yaml
	kubectl apply -f configmap.yaml
	kubectl apply -f serviceaccount.yaml
	kubectl apply -f deployment.yaml
	kubectl apply -f service.yaml

k8s-delete: ## Delete Kubernetes resources
	kubectl delete -f service.yaml --ignore-not-found
	kubectl delete -f deployment.yaml --ignore-not-found
	kubectl delete -f serviceaccount.yaml --ignore-not-found
	kubectl delete -f configmap.yaml --ignore-not-found
	kubectl delete -f namespace.yaml --ignore-not-found

k8s-logs: ## View logs from Kubernetes pods
	kubectl logs -n opensearch-proxy -l app=aws-opensearch-proxy -f

helm-lint: ## Lint Helm chart
	helm lint charts/aws-opensearch-proxy

helm-template: ## Test Helm template rendering
	helm template test charts/aws-opensearch-proxy \
		--set opensearch.url=https://test.us-east-1.es.amazonaws.com

helm-install: ## Install Helm chart (requires opensearch.url)
	@if [ -z "$(OPENSEARCH_URL)" ]; then \
		echo "Error: OPENSEARCH_URL is required"; \
		echo "Usage: make helm-install OPENSEARCH_URL=https://your-domain.us-east-1.es.amazonaws.com"; \
		exit 1; \
	fi
	helm install aws-opensearch-proxy charts/aws-opensearch-proxy \
		--set opensearch.url=$(OPENSEARCH_URL) \
		--create-namespace

helm-upgrade: ## Upgrade Helm release
	helm upgrade aws-opensearch-proxy charts/aws-opensearch-proxy

helm-uninstall: ## Uninstall Helm release
	helm uninstall aws-opensearch-proxy

clean: ## Clean build artifacts
	rm -f aws-opensearch-proxy
	go clean

fmt: ## Format Go code
	gofmt -s -w .

fmt-check: ## Check if code is formatted
	@if [ "$$(gofmt -s -l . | wc -l)" -gt 0 ]; then \
		echo "Code is not formatted. Run 'make fmt' to fix:"; \
		gofmt -s -l .; \
		exit 1; \
	fi
	@echo "✓ Code is properly formatted"

vet: ## Run go vet
	go vet ./...

lint: fmt-check vet ## Run linters

all: lint build ## Run linters and build
