module = github.com/zeta-chain/go-tss

help: ## List of commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

deps: ## Install required dev tooling:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.63.4
	go install github.com/segmentio/golines@v0.12.2

tools: ## Installs tss tools
	go install ./cmd/tss-recovery
	go install ./cmd/tss-benchgen
	go install ./cmd/tss-benchsign

install: go.sum ## Compiles & install go-tss binary
	go install ./cmd/tss

go.sum: go.mod ## Go sum
	@echo "--> Ensure dependencies have not been modified"
	go mod verify

test: ## Runs tests
	@go test -race -timeout 30m ./...

fmt: ## Formats the code
	@echo "Fixing long lines"
	@golines -w --max-len=120 --ignore-generated --ignored-dirs=".git" --base-formatter="gofmt" .

	@echo "Formatting code"
	@golangci-lint run --enable-only 'gci' --enable-only 'gofmt' --enable-only 'whitespace' --fix

lint-pre: ## Runs pre-lint check
	@test -z $(gofmt -l .)
	go mod verify

lint: lint-pre ## Runs linter
	@golangci-lint run

codegen: ## Generates proto
	protoc --go_out=module=$(module):. ./messages/*.proto

.PHONY: deps tools install test fmt lint-pre lint codegen
