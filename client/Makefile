PROTO_INCLUDE ?= $(shell pkg-config --variable=includedir protobuf 2>/dev/null || echo "/usr/include")

.PHONY: build proto proto-update test clean

build:
	go build -o bin/hy ./cmd/hy/
	go build -o bin/bench ./cmd/bench/

proto:
	@mkdir -p pb
	protoc -I proto/client -I $(PROTO_INCLUDE) --go_out=pb --go_opt=paths=source_relative proto/client/*.proto

test:
	go test ./...

clean:
	rm -rf bin/ pb/

proto-update:
	git submodule update --remote proto/client
	@mkdir -p pb
	protoc -I proto/client -I $(PROTO_INCLUDE) --go_out=pb --go_opt=paths=source_relative proto/client/*.proto
