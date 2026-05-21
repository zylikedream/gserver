.DEFAULT_GOAL := build

HELM ?= helm
KRUISE_IMAGE_REPO ?= openkruise-registry.cn-shanghai.cr.aliyuncs.com/openkruise/kruise-manager
OKG_IMAGE_REPO ?= registry-cn-hangzhou.ack.aliyuncs.com/acs/kruise-game-manager
OKG_IMAGE_TAG ?= $(shell git rev-parse --short HEAD 2>/dev/null)
ifneq (, $(shell git status --porcelain 2>/dev/null))
OKG_IMAGE_TAG := $(OKG_IMAGE_TAG).dirty
endif
OKG_IMAGE_TAG := $(if $(TAG),$(TAG),$(OKG_IMAGE_TAG))
OKG_IMAGE ?= game-server:$(OKG_IMAGE_TAG)
OKG_SERVICES ?= role chat friend guild
OKG_DOCKERFILE ?= deploy/Dockerfile.runtime
KIND_CLUSTER ?= game-cluster

# Update GoFrame and its CLI to latest stable version.
.PHONY: up
up: cli.install
	@gf up -a

# Build binary using configuration from hack/config.yaml.
.PHONY: build
build: cli.install
	@gf build -ew

# Simulate a large number of role nodes in the registry.
# Usage: make role-node-sim ARGS="--count 1000"
.PHONY: role-node-sim
role-node-sim:
	@go run ./cmd/role-node-sim $(ARGS)

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


# Build, load into kind, apply K8s manifests, and rollout restart all services.
# Usage: make deploy-k8s
.PHONY: install-okg
install-okg:
	@echo "=== Installing OpenKruiseGame dependencies ==="
	$(HELM) repo add openkruise https://openkruise.github.io/charts/ || true
	$(HELM) repo update
	$(HELM) upgrade --install kruise openkruise/kruise --version 1.8.0 --set manager.image.repository=$(KRUISE_IMAGE_REPO)
	$(HELM) upgrade --install kruise-game openkruise/kruise-game --version 1.0.0 --set prometheus.enabled=false --set image.repository=$(OKG_IMAGE_REPO) --set image.pullPolicy=IfNotPresent
	kubectl patch deployment kruise-controller-manager -n kruise-system --type='json' -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
	kubectl patch daemonset kruise-daemon -n kruise-system --type='json' -p='[{"op":"replace","path":"/spec/template/spec/containers/0/imagePullPolicy","value":"IfNotPresent"}]'
	@echo "=== Waiting for OKG CRDs ==="
	kubectl wait --for=condition=Established crd/gameserversets.game.kruise.io --timeout=120s
	kubectl wait --for=condition=Established crd/gameservers.game.kruise.io --timeout=120s

# Build, load into kind, and deploy game service pools through OpenKruiseGame.
# Usage: make deploy-k8s-okg [TAG=dev-001]
.PHONY: deploy-k8s-okg
deploy-k8s-okg: install-okg
	@$(MAKE) build-okg-image OKG_IMAGE=$(OKG_IMAGE)
	@echo ""
	@echo "=== Loading image into kind ==="
	kind load docker-image $(OKG_IMAGE) --name $(KIND_CLUSTER)
	@echo ""
	@$(MAKE) apply-k8s-okg
	@$(MAKE) update-okg-image OKG_IMAGE=$(OKG_IMAGE)

# Apply OpenKruiseGame manifests using the image already loaded into kind.
# Usage: make apply-k8s-okg
.PHONY: apply-k8s-okg
apply-k8s-okg:
	@echo "=== Applying shared K8s manifests ==="
	kubectl apply -f deploy/k8s/config/
	kubectl apply -f deploy/k8s/game-service-rbac.yaml
	kubectl apply -f deploy/k8s/prometheus.yaml
	kubectl apply -f deploy/k8s/gate-service.yaml
	@echo ""
	@echo "=== Removing old StatefulSet game service pools to avoid double-running ==="
	kubectl delete statefulset role chat friend guild --ignore-not-found
	@echo ""
	@echo "=== Applying OKG GameServerSets ==="
	kubectl apply -f deploy/k8s/role-gameserverset.yaml
	kubectl apply -f deploy/k8s/chat-gameserverset.yaml
	kubectl apply -f deploy/k8s/friend-gameserverset.yaml
	kubectl apply -f deploy/k8s/guild-gameserverset.yaml
	kubectl apply -f deploy/k8s/gate-deployment.yaml
	@echo ""
	@echo "=== Waiting for gate rollout ==="
	kubectl rollout status deployment/gate --timeout=120s
	@echo ""
	@$(MAKE) status-k8s-okg

# Delete OpenKruiseGame game service pools while leaving shared services intact.
# Usage: make delete-k8s-okg
.PHONY: delete-k8s-okg
delete-k8s-okg:
	kubectl delete gameserverset role chat friend guild --ignore-not-found

# Show OpenKruiseGame resources and current game service pods.
# Usage: make status-k8s-okg
.PHONY: status-k8s-okg
status-k8s-okg:
	@echo "=== GameServerSets ==="
	kubectl get gss
	@echo ""
	@echo "=== GameServers ==="
	kubectl get gs
	@echo ""
	@echo "=== Game service pods ==="
	kubectl get pods -l app.kubernetes.io/part-of=gserver -o wide

# Update OpenKruiseGame GameServerSet container images.
# Usage: make update-okg-image [TAG=dev-001]
.PHONY: update-okg-image
update-okg-image:
	@echo "=== Updating OKG image to $(OKG_IMAGE) ==="
	$(eval _RESTART_AT := $(shell date +%s))
	@for svc in $(OKG_SERVICES); do \
		echo "Updating $$svc..."; \
		kubectl patch gss $$svc --type json \
			-p="[{\"op\":\"replace\",\"path\":\"/spec/gameServerTemplate/spec/containers/0/image\",\"value\":\"$(OKG_IMAGE)\"},{\"op\":\"add\",\"path\":\"/spec/gameServerTemplate/metadata/annotations/restartAt\",\"value\":\"$(_RESTART_AT)\"}]"; \
	done
	@$(MAKE) status-k8s-okg

# Build, load, and update only OKG game service images.
# Usage: make build-update-okg-image [TAG=dev-001]
.PHONY: build-update-okg-image
build-update-okg-image:
	@$(MAKE) build-okg-image OKG_IMAGE=$(OKG_IMAGE)
	@echo ""
	@echo "=== Loading image into kind ==="
	kind load docker-image $(OKG_IMAGE) --name $(KIND_CLUSTER)
	@echo ""
	@$(MAKE) update-okg-image OKG_IMAGE=$(OKG_IMAGE)

# Build OKG runtime image from a locally compiled Go binary.
# Usage: make build-okg-image [TAG=dev-001]
.PHONY: build-okg-image
build-okg-image:
	@echo "=== Building linux server binary ==="
	mkdir -p temp/docker
	CGO_ENABLED=0 GOOS=linux GOARCH=$$(go env GOARCH) go build -o temp/docker/server ./node/
	@echo ""
	@echo "=== Building docker image $(OKG_IMAGE) ==="
	docker build -f $(OKG_DOCKERFILE) -t $(OKG_IMAGE) .

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
	@if [ ! -d gameconfig/gosrc ]; then git submodule update --init gameconfig; fi
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

.PHONY: docker
docker: cli.install
	@docker build -t game-server:latest .


# Generate protobuf files for database tables.
.PHONY: pbentity
pbentity: cli.install
	@gf gen pbentity
