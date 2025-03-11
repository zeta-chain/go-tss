module = github.com/zeta-chain/go-tss

help: ## List of commands
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

all: lint build

tools:
	go install ./cmd/tss-recovery
	go install ./cmd/tss-benchgen
	go install ./cmd/tss-benchsign

install: go.sum
	go install ./cmd/tss

go.sum: go.mod
	@echo "--> Ensure dependencies have not been modified"
	go mod verify

test:
	@go test --race -timeout 30m ./...

lint-pre:
	@gofumpt -l cmd common keygen keysign messages p2p storage tss # for display
	@test -z "$(shell gofumpt -l cmd common keygen keysign messages p2p storage tss)" # cause error
	@go mod verify

lint: lint-pre
	@golangci-lint run

lint-verbose: lint-pre
	@golangci-lint run -v

codegen:
	protoc --go_out=module=$(module):. ./messages/*.proto

build: codegen
	go build ./...

.PHONY: all tools install test lint-pre lint lint-verbose codegen build
