# Configuration

**English** | [Русский](../ru/configuration.md)

The only source of truth for VIPs is the `VIPPool` and `VIPBinding` CRDs.
Service annotations (`easylb.io/vips`, `easylb.io/vip-config`) have been
removed and are ignored.

## VIPPool

A cluster-scoped address pool.

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPPool
metadata:
  name: ingress-vips
spec:
  addresses:
    - 192.168.56.240-192.168.56.249
    - 192.168.57.10/32
  autoAssign: true
  mode: l2
  serviceAllocation:
    namespaceSelector:
      matchLabels:
        kubernetes.io/metadata.name: ingress-nginx
    serviceSelector:
      matchLabels:
        app.kubernetes.io/name: ingress-nginx
```

Fields:

| Field | Description |
|---|---|
| `spec.addresses` | list of IPs, CIDRs, or `a-b` ranges; IPv4 and IPv6 |
| `spec.autoAssign` | enable automatic allocation (default `true`) |
| `spec.avoidBuggyIPs` | skip IPv4 addresses ending in `.0`/`.255` |
| `spec.mode` | default mode for auto-created bindings: `l2` (default) or `external` |
| `spec.serviceAllocation` | optional `namespaceSelector`/`serviceSelector` for auto-allocation |

Limits:

- a pool expands to at most 4096 addresses;
- addresses are not checked against the real network, only against other
  `VIPBinding` claims;
- one pool can be used by several bindings.

## Automatic Service allocation {#auto-allocation}

With `autoAssign: true` (the default), easyLB acts like MetalLB L2: it watches
`LoadBalancer` Services and creates a `VIPBinding` for a Service that does not
have one yet. This removes the need to create a binding per Service.

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPPool
metadata:
  name: default-vips
spec:
  addresses:
    - 192.168.56.240-192.168.56.249
  autoAssign: true
  mode: l2
  serviceAllocation:
    namespaceSelector:
      matchLabels:
        environment: production
    serviceSelector:
      matchLabels:
        team: platform
```

Behavior:

- the controller creates `auto-<service-name>` in the Service namespace with an
  owner reference to the Service;
- deleting the Service deletes the binding and releases the address;
- if a manual or Gateway-created binding already exists, auto-allocation is
  skipped, and an existing auto binding is deleted so the explicit binding
  wins;
- `serviceAllocation` is optional. When unset, the pool is available to all
  Services. An empty selector matches everything;
- if `metallb.io/address-pool` (or the legacy
  `metallb.universe.tf/address-pool`) names a pool on the Service, that pool is
  used explicitly and `autoAssign` is not required;
- `easylb.io/auto-assign: "false"` on the Service disables auto-allocation for
  it;
- `controller.autoAssignServices: false` disables the feature globally;
- matching pools are tried in name order; an exhausted pool is skipped in
  favor of the next matching pool;
- `avoidBuggyIPs: true` skips IPv4 addresses ending in `.0`/`.255`;
- when a pool disappears or stops matching, an existing auto binding is kept so
  traffic is not interrupted. Delete the binding manually to release the VIP.

MetalLB compatibility for auto-allocated Services:

| Service field / annotation | Effect |
|---|---|
| `spec.loadBalancerIP` | requested address; uses a pool when possible |
| `metallb.io/loadBalancerIPs` | comma-separated requested addresses |
| `metallb.universe.tf/loadBalancerIPs` | legacy alias |
| `metallb.io/address-pool` | select a VIPPool by name |
| `metallb.universe.tf/address-pool` | legacy alias |
| `metallb.io/allow-shared-ip` | share a VIP with other Services using the same key |
| `metallb.universe.tf/allow-shared-ip` | legacy alias |

Multiple requested addresses are stored as explicit `VIPBinding.spec.vips`.
Services with the same `metallb.io/allow-shared-ip` (or legacy
`metallb.universe.tf/allow-shared-ip`) key share a VIP when their ports do not
overlap; auto-created bindings set `spec.sharingKey` automatically.
`loadBalancerSourceRanges` is not implemented yet.

## VIPBinding

A namespaced object, usually in the Service namespace.

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: ingress-easylb
  namespace: ingress-nginx
spec:
  serviceRef:
    name: my-release-ingress-nginx-controller
  mode: l2
  poolRef:
    name: ingress-vips
```

Fields:

| Field | Description |
|---|---|
| `spec.serviceRef.name` | `Service` name in the same namespace |
| `spec.mode` | `external` (default), `l2`; `bgp` is reserved |
| `spec.vips` | explicit VIP list; mutually exclusive with `poolRef` |
| `spec.poolRef.name` | `VIPPool` name |
| `spec.requestedIP` | a specific IP from the pool; only with `poolRef` |
| `spec.sharingKey` | allow sharing a VIP between Services with the same key and non-overlapping ports |

Rules:

- either `vips` or `poolRef`; setting both is an error;
- the same IP cannot be claimed by two bindings unless both set the same
  non-empty `sharingKey` and their Services expose non-overlapping ports;
- `mode` applies to the whole binding. For a mixed case (some VIPs on
  keepalived, some on easyLB), create **two bindings** for the same Service.

### Explicit VIPs without a pool

A `VIPPool` is optional. If you already know the VIP, set `spec.vips` directly
and do not use `poolRef`/`requestedIP`:

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: ingress-keepalived
  namespace: ingress-nginx
spec:
  serviceRef:
    name: my-release-ingress-nginx-controller
  mode: external
  vips:
    - 192.168.56.250
```

This is the minimal setup for the original keepalived problem: keepalived owns
the VIP, easyLB publishes it in `Service.status.loadBalancer.ingress`, and
kube-proxy preserves the client source IP with
`externalTrafficPolicy: Local`. No pool and no speaker are needed.

For an easyLB-owned VIP, use `mode: l2` with the same explicit `vips` and keep
the speaker enabled. Automatic allocation only works with a `VIPPool`; a manual
binding is never overwritten by it.

### Status

```yaml
status:
  allocatedIPs:
    - 192.168.56.240
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: Allocated
      message: address allocated from pool
```

Possible `reason` values:

| Reason | Meaning |
|---|---|
| `Explicit` | VIPs come from `spec.vips` |
| `Allocated` | an address was allocated from a pool |
| `InvalidSpec` | broken rules (vips+poolRef, missing vips/poolRef, etc.) |
| `InvalidVIP` | invalid IP |
| `Conflict` | the IP is already claimed by another binding |
| `PoolNotFound` | pool not found; the previous allocation is kept |
| `PoolExhausted` | no free addresses in the pool |
| `PoolAutoAssignDisabled` | `autoAssign: false` without `requestedIP` |
| `RequestedIPUnavailable` | `requestedIP` is outside the pool or taken |
| `AllocationError` | API/read error |

On a transient pool error, easyLB keeps the previous allocation instead of
clearing it, so traffic is not interrupted.

## Examples

### Keepalived VIP (external owner)

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: ingress-keepalived
  namespace: ingress-nginx
spec:
  serviceRef:
    name: my-release-ingress-nginx-controller
  mode: external
  vips:
    - 192.168.56.250
```

easyLB only publishes `.250` in the status. The VIP assignment, ARP/VRRP, and
failover are handled by keepalived. The speaker ignores this binding.

### easyLB L2 VIP from a pool

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: ingress-easylb
  namespace: ingress-nginx
spec:
  serviceRef:
    name: my-release-ingress-nginx-controller
  mode: l2
  poolRef:
    name: ingress-vips
  # requestedIP: 192.168.56.240
```

The controller allocates the first free address from the pool and writes it to
`status.allocatedIPs`. The speaker advertises it over ARP.

### Two VIPs for one Service (mixed mode)

Create two bindings as in the examples above. The controller merges the VIPs
and publishes both in the same `Service.status.loadBalancer.ingress`.

## Node IPs

Controlled by a controller flag (and the Helm value):

```yaml
controller:
  publishNodeIPs: true
  nodeIPTypes: InternalIP
```

- `true` — node IPs are added to the status together with VIPs. Required if
  DNS points at node IPs.
- `false` — only VIPs from bindings are published. DNS to node IPs stops
  working for this Service (unless the IPs are in `spec.externalIPs`).
- `nodeIPTypes` — `InternalIP`, `ExternalIP`, or both separated by commas.
- with `Local`, only Ready nodes with a Ready endpoint are published;
- with `Cluster`, all Ready nodes are published.

## Controller flags

| Flag | Default | Purpose |
|---|---|---|
| `--load-balancer-class` | `easylb.io/easylb` | Service class |
| `--require-class` | `true` | require `spec.loadBalancerClass` |
| `--enable-gateway-api` | `false` | watch `gateway.networking.k8s.io/v1` `Gateway` and create `VIPBinding` |
| `--auto-assign-services` | `true` | create `VIPBinding` automatically for matching Services |
| `--node-ip-types` | `InternalIP` | node address types to publish |
| `--publish-node-ips` | `true` | publish node IPs |
| `--leader-elect` | `false` | leader election |
| `--leader-election-namespace` | `easylb-system` | Lease namespace |
| `--leader-election-lease-duration` | `15s` | Lease duration |
| `--leader-election-renew-deadline` | `10s` | renew deadline |
| `--leader-election-retry-period` | `2s` | retry period |
| `--requeue-after` | `5m` | periodic resync |

## Speaker flags

| Flag | Default | Purpose |
|---|---|---|
| `--lease-namespace` | `easylb-system` | per-VIP Lease namespace |
| `--node-ip-type` | `InternalIP` | node address used to select the ARP interface |
| `--interface` | empty | ARP interface; auto-detected when empty |
| `--lease-duration` | `15s` | Lease duration |
| `--renew-deadline` | `10s` | renew deadline |
| `--retry-period` | `2s` | retry period |
| `--requeue-after` | `30s` | periodic resync |

## Helm values

Structure:

```yaml
controller:
  enabled: true
  replicas: 2
  image:
    repository: <registry>/easylb
    tag: <tag>
  publishNodeIPs: true
  nodeIPTypes: InternalIP
  requireClass: false        # needed for Gateway data-plane Services without a class
  autoAssignServices: true
  leaderElect: true
  pdb:
    enabled: true
    minAvailable: 1

speaker:
  enabled: true
  image:
    repository: <registry>/easylb
    tag: <tag>
  interface: ""
  nodeIPType: InternalIP

gatewayAPI:
  enabled: false

metrics:
  service:
    enabled: true
  serviceMonitor:
    enabled: false

pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true
    mode: l2
    serviceAllocation:
      namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: ingress-nginx

bindings:
  ingress-keepalived:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
  ingress-easylb:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: l2
    poolRef: ingress-vips
```

`pools` and `bindings` are maps: the key becomes the object name. A complete
example is in `charts/easylb/values-example.yaml`.

For the native Gateway API integration, set `gatewayAPI.enabled: true` and
install the Gateway API `v1` CRDs first. Gateway implementation data-plane
Services usually have no `loadBalancerClass`; either keep
`controller.requireClass: false` (easyLB then manages every classless
`LoadBalancer` Service in the cluster) or set `loadBalancerClass:
easylb.io/easylb` on that Service explicitly.

## Helm and CRDs

- CRDs live in `charts/easylb/crds/` and are installed on the first
  `helm install`.
- `helm upgrade` **does not update** CRDs from `crds/`. When changing the
  schema, apply `deploy/crds.yaml` separately.
- `helm uninstall` does not delete CRDs (standard Helm behavior).

## Migration from annotations

Annotations have been removed. If a Service still has `easylb.io/vips` or
`easylb.io/vip-config`, they are ignored. Create a `VIPBinding` for the same
Service; the controller will start publishing its VIPs.

## Conflicts

- One VIP cannot be in two bindings unless they share a `sharingKey` and
  expose non-overlapping ports.
- One VIP cannot be in `mode: l2` and on keepalived at the same time.
- The same `VIP:port` cannot be used by two Services. Services with the same
  `sharingKey` may share a VIP on different ports; for multiple domains on one
  port, use one ingress with Host/SNI routing.
