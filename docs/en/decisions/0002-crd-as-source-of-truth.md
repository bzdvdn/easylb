# 0002 — CRDs as the single source of truth

**Status:** Accepted

## Problem

VIPs must be configured without modifying the ingress-nginx or Gateway
data-plane Service, and the configuration must support automatic allocation and
status.

## Constraints

- The Service is often owned by Helm or a Gateway implementation.
- Pool allocation needs persistent state.
- Helm should install and template the configuration cleanly.

## Options

1. Service annotations (`easylb.io/vips`, `easylb.io/vip-config`).
2. A ConfigMap with a global VIP list.
3. CRDs: `VIPPool` and `VIPBinding`.

## Decision

Option 3: CRDs only.

## Why

- Typed and validated by the API server.
- One owner per VIP, no precedence rules between annotations and bindings.
- `VIPBinding.status.allocatedIPs` is a natural place for allocation state.
- Namespaced `VIPBinding` fits RBAC and multi-tenancy.
- Helm can ship CRDs in `crds/` and bindings as templates without touching
  third-party Services.

## Consequences

- CRDs must be installed before the controller/speaker.
- `helm upgrade` does not update CRDs from `crds/`.
- Users migrating from annotations must create `VIPBinding` objects.

## Re-evaluation trigger

Need for a zero-CRD mode in environments where CRDs cannot be installed.
