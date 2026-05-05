.PHONY: build proto proto-update test clean

build:
	go build -o bin/hy ./cmd/hy/

proto:
	@mkdir -p pb
	protoc -I proto/client --go_out=pb --go_opt=paths=source_relative proto/client/*.proto

test:
	go test ./...

clean:
	rm -rf bin/ pb/

proto-update:
	git subtree pull --prefix=proto/client proto master  --squash
	@mkdir -p pb
	protoc -I proto/client --go_out=pb --go_opt=paths=source_relative proto/client/*.proto
