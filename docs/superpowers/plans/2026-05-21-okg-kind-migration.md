# OKG Kind Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Run the existing role/chat/friend/guild game services through OpenKruiseGame `GameServerSet` resources in kind.

**Architecture:** Keep `gate` as the entry `Deployment` and migrate only game-service instance pools from `StatefulSet` to `GameServerSet`. Preserve current Pod labels, config mounts, probes, ports, Prometheus annotations, and Redis registry behavior, while adding `GS_NAME` for OKG instance identity.

**Tech Stack:** Kubernetes, kind, Helm, OpenKruise, OpenKruiseGame, GNU Make, YAML.

---

### Task 1: Add GameServerSet Manifests

**Files:**
- Create: `deploy/k8s/role-gameserverset.yaml`
- Create: `deploy/k8s/chat-gameserverset.yaml`
- Create: `deploy/k8s/friend-gameserverset.yaml`
- Create: `deploy/k8s/guild-gameserverset.yaml`

- [ ] **Step 1: Create one `GameServerSet` per game-service pool**

Use `apiVersion: game.kruise.io/v1alpha1`, `kind: GameServerSet`, `serviceName`, `replicas: 2`, `updateStrategy.rollingUpdate.podUpdatePolicy: InPlaceIfPossible`, and `gameServerTemplate`.

- [ ] **Step 2: Preserve existing Pod behavior**

Copy the existing StatefulSet Pod template fields for each service: labels, Prometheus annotations, hostAliases, termination grace period, image, args, `POD_IP`, ports, probes, resources, config/log volumes.

- [ ] **Step 3: Add OKG instance identity**

Add `GS_NAME` from `metadata.name` to every game-service container:

```yaml
- name: GS_NAME
  valueFrom:
    fieldRef:
      fieldPath: metadata.name
```

- [ ] **Step 4: Add matching headless Service resources**

Keep `role-svc`, `chat-svc`, `friend-svc`, and `guild-svc`, but set selectors to `game.kruise.io/owner-gss: <name>` so they target OKG pods and avoid selecting old StatefulSet pods.

### Task 2: Add Make Targets

**Files:**
- Modify: `hack/hack.mk`

- [ ] **Step 1: Add `install-okg`**

Use Helm upgrade/install so the command is idempotent:

```bash
helm repo add openkruise https://openkruise.github.io/charts/ || true
helm repo update
helm upgrade --install kruise openkruise/kruise --version 1.8.0
helm upgrade --install kruise-game openkruise/kruise-game --version 1.0.0 --set prometheus.enabled=false
```

- [ ] **Step 2: Add `deploy-k8s-okg`**

Build and load `game-server:latest`, install OKG, apply config and Prometheus manifests, delete old game-service StatefulSets with `--ignore-not-found`, apply the four GameServerSet manifests, then apply `gate`.

- [ ] **Step 3: Add `delete-k8s-okg`**

Delete the four GameServerSet resources with `--ignore-not-found`.

- [ ] **Step 4: Add `status-k8s-okg`**

Show `gss`, `gs`, game-service pods, and gate pods.

### Task 3: Update Deployment Documentation

**Files:**
- Modify: `docs/system/k8s-deployment.md`

- [ ] **Step 1: Add OKG installation commands**

Document `make install-okg` and the equivalent Helm commands.

- [ ] **Step 2: Add OKG deployment flow**

Document `make deploy-k8s-okg`, what it applies, and why it deletes old StatefulSets first.

- [ ] **Step 3: Add validation commands**

Document:

```bash
kubectl get crd | grep -E 'gameserver|gameserverset|kruise'
kubectl get gss
kubectl get gs
kubectl get pods -l app=role
kubectl get pods -l app=chat
kubectl get pods -l app=friend
kubectl get pods -l app=guild
```

### Task 4: Local Verification

**Files:**
- Read: all changed YAML and Makefile targets

- [ ] **Step 1: Validate YAML parsing**

Run:

```bash
kubectl apply --dry-run=client -f deploy/k8s/role-gameserverset.yaml
kubectl apply --dry-run=client -f deploy/k8s/chat-gameserverset.yaml
kubectl apply --dry-run=client -f deploy/k8s/friend-gameserverset.yaml
kubectl apply --dry-run=client -f deploy/k8s/guild-gameserverset.yaml
```

Expected: YAML parses successfully. If CRDs are not installed, server-side validation may still fail later until OKG is installed.

- [ ] **Step 2: Inspect make targets**

Run:

```bash
make -n install-okg
make -n deploy-k8s-okg
make -n delete-k8s-okg
make -n status-k8s-okg
```

Expected: Commands expand without make syntax errors.
