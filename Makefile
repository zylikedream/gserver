# GServer monorepo 根工程接口：server module(gserver)+ client module(gserver/client)。
# 所有开发命令从这里进入；client/Makefile 只作为薄入口。

include hack/hack-cli.mk
include hack/hack.mk

# --- 构建 ---
.PHONY: build
build:
	go build -o bin/gserver-node ./node
	go -C client build -o ../bin/hy ./cmd/hy
	go -C client build -o ../bin/bench ./cmd/bench

# --- 测试 ---
.PHONY: test
test:
	go test ./...
	go -C client test ./...

# --- lint ---
.PHONY: lint
lint:
	golangci-lint run ./...
	cd client && golangci-lint run ./...

# --- client 黑盒 seam 门禁 ---
.PHONY: check-client-boundary
check-client-boundary:
	bash build/script/check_client_boundary.sh

# --- 固定 protobuf 工具链 ---
.PHONY: tools
tools:
	bash build/script/install_protobuf_tools.sh

# --- PB 生成（唯一真源 protocol/client + protocol/server）---
# 工具版本固定为 protoc 3.19.3 + protoc-gen-go v1.36.11，见 install_protobuf_tools.sh。
PROTOC := .tools/bin/protoc
PROTOC_GEN_GO := .tools/bin/protoc-gen-go
PB_INCLUDE := -I .tools/include

# server PB：client + server proto → protocol/pb（保留 server 端去 omitempty 行为）
.PHONY: pb-server
pb-server: tools
	@rm -rf protocol/pb && mkdir -p protocol/pb
	@for f in protocol/client/*.proto protocol/server/*.proto; do \
		$(PROTOC) -I "$$(dirname $$f)" -I protocol/client -I protocol/server $(PB_INCLUDE) \
			--plugin=protoc-gen-go=$(PROTOC_GEN_GO) --go_out=protocol "$$f" || exit 1; \
	done
	@find protocol/pb -name '*.pb.go' -type f -exec sed -i 's/,omitempty"/"/' {} \;

# client PB：仅 client proto → client/pb（source_relative，不含 server internal）
.PHONY: pb-client
pb-client: tools
	@rm -rf client/pb && mkdir -p client/pb
	$(PROTOC) -I protocol/client $(PB_INCLUDE) \
		--plugin=protoc-gen-go=$(PROTOC_GEN_GO) \
		--go_out=client/pb --go_opt=paths=source_relative \
		protocol/client/*.proto

.PHONY: pb
pb: pb-server pb-client
	@echo "pb: server + client generated"

# 重新生成后要求双端 PB 工作区干净（捕获已跟踪 diff 与新增未跟踪 .pb.go）
.PHONY: pb-check
pb-check: pb
	@if [ -n "$$(git status --porcelain -- protocol/pb client/pb)" ]; then \
		echo "PB 漂移：make pb 后 protocol/pb 或 client/pb 有未提交变更" >&2; \
		git status --porcelain -- protocol/pb client/pb >&2; \
		exit 1; \
	fi
	@echo "PB 生成一致，无漂移"

# --- 真实 E2E（先构建 hy 再统一编排）---
.PHONY: e2e
e2e: build
	bash build/script/e2e_all.sh
