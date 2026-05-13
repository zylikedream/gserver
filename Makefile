include hack/hack-cli.mk
include hack/hack.mk

# Run all tests (gcflags=-l disables inlining for gomonkey compatibility).
.PHONY: test
test:
	go test -gcflags=-l ./...