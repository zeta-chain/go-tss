clear:
	clear
all: lint build

test:
	@go test -mod=readonly ./...

test-watch: clear
	@./scripts/watch.bash

lint-pre:
	@test -z $(gofmt -l .) # checks code is in proper format
	@go mod verify

lint: lint-pre
	@golangci-lint run

lint-verbose: lint-pre
	@golangci-lint run -v

build:
	@go build ./...
