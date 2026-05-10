.DEFAULT_GOAL := build

# Update GoFrame and its CLI to latest stable version.
.PHONY: up
up: cli.install
	@gf up -a

# Build binary using configuration from hack/config.yaml.
.PHONY: build
build: cli.install
	@gf build -ew

# Parse api and generate controller/sdk.
.PHONY: ctrl
ctrl: cli.install
	@gf gen ctrl

# Generate Go files for DAO/DO/Entity.
.PHONY: dao
dao: cli.install
	@gf gen dao

# Parse current project go files and generate enums go file.
.PHONY: enums
enums: cli.install
	@gf gen enums

# Generate Go files for Service.
.PHONY: service
service: cli.install
	@gf gen service


# Build docker image.
.PHONY: image
image: cli.install
	$(eval _TAG  = $(shell git rev-parse --short HEAD))
ifneq (, $(shell git status --porcelain 2>/dev/null))
	$(eval _TAG  = $(_TAG).dirty)
endif
	$(eval _TAG  = $(if ${TAG},  ${TAG}, $(_TAG)))
	$(eval _PUSH = $(if ${PUSH}, ${PUSH}, ))
	@gf docker ${_PUSH} -tn $(DOCKER_NAME):${_TAG};


# Build docker image and automatically push to docker repo.
.PHONY: image.push
image.push: cli.install
	@make image PUSH=-p;


# Deploy image and yaml to current kubectl environment.
.PHONY: deploy
deploy: cli.install
	$(eval _TAG = $(if ${TAG},  ${TAG}, develop))

	@set -e; \
	mkdir -p $(ROOT_DIR)/temp/kustomize;\
	cd $(ROOT_DIR)/manifest/deploy/kustomize/overlays/${_ENV};\
	kustomize build > $(ROOT_DIR)/temp/kustomize.yaml;\
	kubectl   apply -f $(ROOT_DIR)/temp/kustomize.yaml; \
	if [ $(DEPLOY_NAME) != "" ]; then \
		kubectl patch \
		-n $(NAMESPACE) deployment/$(DEPLOY_NAME) \
		-p "{\"spec\":{\"template\":{\"metadata\":{\"labels\":{\"date\":\"$(shell date +%s)\"}}}}}"; \
	fi;


# Update subtree: protocol
.PHONY: upproto
upproto:
	git subtree pull --prefix=protocol/client git@gitee.com:zylikedream/mahong-protocol.git master; \

# Push protocol changes to remote
.PHONY: pushproto
pushproto:
	@git subtree push --prefix=protocol/client git@gitee.com:zylikedream/mahong-protocol.git master

# Update subtree: gameconfig
.PHONY: upcfg
upcfg:
	git subtree pull --prefix=gameconfig https://gitee.com/zylikedream/garden_config_go.git master; \

# Parsing protobuf files and generating go files.
.PHONY: pbraw
pbraw: cli.install
	@for dir in protocol/client protocol/server; do \
		for file in `ls $$dir/*.proto 2>/dev/null`; do \
			echo "Generating $$file"; \
			protoc --proto_path=$$dir/ -I $$dir/ -I protocol/client/ -I protocol/server/ -I /usr/local/include --go_out=protocol/ $$file; \
		done; \
	done;

# 生成不带omitempty标签的protobuf代码
.PHONY: pb
pb: cli.install
	@find protocol -name "*.pb.go" -delete
	@$(MAKE) pbraw
	@echo "Removing omitempty tags from generated protobuf files..."
	@find protocol/pb -name "*.pb.go" -type f -exec sed -i 's/,omitempty"/"/' {} \;

# 列出每个proto文件的消息ID范围
.PHONY: pbids
pbids:
	@for f in protocol/client/*.proto; do \
		max=$$(grep "option (msg_id)" "$$f" 2>/dev/null | sed 's/.*= *//;s/;//' | sort -n | tail -1); \
		if [ -n "$$max" ]; then \
			printf "%-30s %s\n" "$$(basename $$f)" "$$max"; \
		fi; \
	done | sort -t' ' -k2 -n; \
	max=$$(grep -rh "option (msg_id)" protocol/client/*.proto | sed 's/.*= *//;s/;//' | sort -n | tail -1); \
	prefix=$$(((max / 1000 + 1)*10)); \
	echo "---"; \
	echo "max: $$max"; \
	echo "next range: $${prefix}01~$${prefix}99";

# 新建协议文件，用法: make newproto MOD=xxx
.PHONY: newproto
newproto:
	@test -n "$(MOD)" || (echo "usage: make newproto MOD=xxx" && false)
	@prefix=$$(grep -rh "option (msg_id)" protocol/client/*.proto | sed 's/.*= *//;s/;//' | sort -n | tail -1 | sed 's/^0*//'); \
	prefix=$$(grep -rh "option (msg_id)" protocol/client/*.proto | sed 's/.*= *//;s/;//' | sort -n | tail -1 | sed 's/^0*//'); \
	prefix=$$((($$prefix / 1000 + 1) * 10)); \
	file="protocol/client/$(MOD).proto"; \
	test -f "$$file" && (echo "file exists: $$file" && exit 1) || true; \
	echo "// ID: $${prefix}01~$${prefix}99" > $$file; \
	echo 'syntax = "proto3";' >> $$file; \
	echo 'option go_package="./pb;pb";' >> $$file; \
	echo 'package galaxy.protocol;' >> $$file; \
	echo '' >> $$file; \
	echo 'import "msg_options.proto";' >> $$file; \
	echo "" >> $$file; \
	echo "created: $$file (ID range: $${prefix}01~$${prefix}99)"

# Generate protobuf files for database tables.
.PHONY: pbentity
pbentity: cli.install
	@gf gen pbentity

# Convert subtree to submodule
.PHONY: submodule-proto
submodule-proto:
	@echo "Pushing current subtree state to remote..."
	git subtree push --prefix=protocol/client git@gitee.com:zylikedream/mahong-protocol.git master
	@echo "Removing subtree..."
	git rm -r --cached protocol/client
	rm -rf protocol/client
	@echo "Adding as submodule..."
	git submodule add git@gitee.com:zylikedream/mahong-protocol.git protocol/client
	git commit -m "chore: convert protocol/client from subtree to submodule"

.PHONY: submodule-cfg
submodule-cfg:
	@echo "Pushing current subtree state to remote..."
	git subtree push --prefix=gameconfig https://gitee.com/zylikedream/garden_config_go.git master
	@echo "Removing subtree..."
	git rm -r --cached gameconfig
	rm -rf gameconfig
	@echo "Adding as submodule..."
	git submodule add https://gitee.com/zylikedream/garden_config_go.git gameconfig
	git commit -m "chore: convert gameconfig from subtree to submodule"
