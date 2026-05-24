# OpenKruiseGame Kind Migration Design

## Goal

Migrate the local kind deployment toward OpenKruiseGame (OKG) while keeping the first step small and reversible.

The immediate target is to run the existing game service pods through OKG `GameServerSet` resources in kind. This phase does not change business code, Redis registry behavior, database configuration, Prometheus scraping, or the gateway entry path.

## Scope

In scope:

- Install OpenKruise / OpenKruiseGame CRDs and controllers into the kind cluster.
- Add `GameServerSet` manifests for `role`, `chat`, `friend`, and `guild`.
- Keep existing `StatefulSet` manifests for rollback and comparison.
- Keep `gate` as a normal `Deployment` with the existing NodePort `Service`.
- Keep Redis, Prometheus, Grafana datasource, and ConfigMap structure unchanged.
- Update local deployment documentation and helper targets so the OKG path is repeatable.

Out of scope for this phase:

- GameServer state reporting from application code.
- OKG network model integration.
- Autoscaling strategy.
- ACK, Tair, PolarDB, SLS, or other Alibaba Cloud production wiring.
- Replacing Redis registry or actor location logic.

## Current State

The current kind deployment uses:

- `StatefulSet` for `role`, `chat`, `friend`, and `guild`.
- `Deployment` for `gate`.
- `Deployment` for local Redis and Prometheus.
- ConfigMaps under `deploy/k8s/config/`.
- Redis service registry based on `POD_IP`.
- Prometheus annotations on service pods.

The services are already shaped in a way that maps cleanly to OKG because each game service pod is self-contained and gets runtime config from ConfigMap.

## Target Resource Model

`role`, `chat`, `friend`, and `guild` get new manifests:

- `deploy/k8s/role-gameserverset.yaml`
- `deploy/k8s/chat-gameserverset.yaml`
- `deploy/k8s/friend-gameserverset.yaml`
- `deploy/k8s/guild-gameserverset.yaml`

Each `GameServerSet` preserves the current workload behavior:

- `replicas: 2`
- same `app` labels
- same Prometheus scrape annotations
- same container image and `imagePullPolicy: IfNotPresent`
- same config argument, for example `--config /config/role.toml`
- same `POD_IP` environment variable
- same actor and metrics ports
- same readiness and liveness probes
- same resource requests and limits
- same ConfigMap and log volumes
- same `hostAliases`
- same `terminationGracePeriodSeconds: 30`

The existing headless services stay in place:

- `role-svc`
- `chat-svc`
- `friend-svc`
- `guild-svc`

They are retained for DNS compatibility and future options, even though the current service discovery path is Redis registry plus `POD_IP`.

`gate` remains:

- `deploy/k8s/gate-deployment.yaml`
- `deploy/k8s/gate-service.yaml`

The gateway is not migrated to OKG in this phase because it is an entry gateway, not a game server instance pool.

## Deployment Flow

The local OKG deployment flow should be:

1. Create or reuse the kind cluster.
2. Install OpenKruise / OpenKruiseGame CRDs and controllers.
3. Build and load the local `game-server:latest` image into kind.
4. Apply ConfigMaps.
5. Apply Redis and Prometheus.
6. Apply OKG `GameServerSet` manifests for `role`, `chat`, `friend`, and `guild`.
7. Apply `gate` deployment and service.
8. Verify OKG resources, pods, Redis registration, and Prometheus metrics.

The existing non-OKG deployment flow stays available for fallback.

## Validation

Minimum validation commands:

```bash
kubectl get crd | grep -E 'gameserver|gameserverset|kruise'
kubectl get gss
kubectl get gs
kubectl get pods -l app=role
kubectl get pods -l app=chat
kubectl get pods -l app=friend
kubectl get pods -l app=guild
kubectl logs -l app=role --tail=100
curl http://localhost:30999/-/ready
```

Functional success criteria:

- OKG controllers are running.
- `GameServerSet` resources create the expected pods.
- Pods pass readiness and liveness probes.
- Services register into Redis using pod IP.
- Prometheus sees `role`, `chat`, `friend`, and `guild` metrics.
- Existing gateway login path still works through `gate`.

## Rollback

Rollback stays simple because existing StatefulSet manifests are not removed.

Rollback steps:

1. Delete the OKG `GameServerSet` resources.
2. Re-apply existing `*-statefulset.yaml` manifests.
3. Confirm Redis service registry has fresh entries from the StatefulSet pods.
4. Confirm Prometheus metrics and gateway login still work.

## Risks

The main risk is manifest shape mismatch with the installed OKG version. This is controlled by first validating the CRD schema in kind and keeping the migration as additive manifests instead of editing the current StatefulSets in place.

Another risk is accidental double-running of StatefulSet and GameServerSet pods with the same `app` labels. The deployment commands must avoid applying both paths at the same time unless intentionally comparing behavior.

## Next Step

After this spec is approved, create an implementation plan for:

- OKG installation command or helper target.
- New `GameServerSet` manifests.
- Deployment documentation updates.
- Verification commands.
