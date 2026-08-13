include hack/hack-cli.mk
include hack/hack.mk

# Run all tests.
.PHONY: test
test:
	go test ./...

# Lint Go code using golangci-lint.
.PHONY: lint
lint:
	golangci-lint run ./...