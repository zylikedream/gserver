# client module(gserver/client)薄入口。
# 统一工程命令（build/test/lint/pb/e2e）由根 Makefile 提供；此处仅保留本地直达。
# PB 生成唯一真源为根 protocol/client，禁止 client 内 submodule 更新。

.PHONY: build test clean

build:
	go build -o bin/hy ./cmd/hy/
	go build -o bin/bench ./cmd/bench/

test:
	go test ./...

clean:
	rm -rf bin/ pb/
