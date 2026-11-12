# easyLB roadmap

easyLB targets small bare-metal / on-prem clusters that need a simple
`LoadBalancer` implementation without BGP. The goal is to replace the k3s/RKE2
ServiceLB and MetalLB's **L2 mode**, not to reimplement MetalLB's BGP and
advanced traffic-engineering features.

Status: `[x]` done, `[ ]` not done yet.

## Done

### Core

- [x] Controller publishes node IPs and VIPs into
  `Service.status.loadBalancer.ingress` without `svclb` pods or hostPort.
- [x] `VIPPool` and `VIPBinding` CRDs.
- [x] Explicit VIPs without a pool: `VIPBinding.spec.vips` works for both
  `external` and `l2` modes.
- [x] `external` mode for keepalived/VRRP, hardware LBs, and any externally
  managed VIP.
- [x] Source IP preservation with `externalTrafficPolicy: Local` and local
  Ready endpoints.
- [x] Controller HA: 2 replicas, leader election, PDB, topology spread, and
  optimistic status updates.
- [x] Service selection via `loadBalancerClass` and `requireClass`, plus
  `easylb.io/enabled: "false"` opt-out.

### IPAM and automatic allocation

- [x] `VIPPool` addresses: IPs, CIDRs, and `a-b` ranges.
- [x] Automatic allocation from a pool (`autoAssign`, default `true`).
- [x] `requestedIP` from a pool.
- [x] Automatic `VIPBinding` creation for matching `LoadBalancer` Services
  (`auto-<service>`, ownerReference to the Service).
- [x] `VIPPool.spec.serviceAllocation` namespace/service selectors.
- [x] `VIPPool.spec.mode` for auto-created bindings (`l2` default, `external`
  supported).
- [x] `avoidBuggyIPs` (skip IPv4 `.0`/`.255`).
- [x] Fallback to the next matching pool when the selected pool is exhausted.
- [x] Manual and Gateway-created bindings take precedence over auto-allocation.
- [x] Automatic bindings are garbage-collected with the Service.
- [x] Pool selection is deterministic by pool name; existing auto bindings are
  kept when a pool disappears to avoid dropping traffic.

### MetalLB L2 compatibility

- [x] `spec.loadBalancerIP`.
- [x] `metallb.io/loadBalancerIPs` and legacy
  `metallb.universe.tf/loadBalancerIPs`.
- [x] `metallb.io/address-pool` and legacy
  `metallb.universe.tf/address-pool`.
- [x] `allow-shared-ip` via `VIPBinding.spec.sharingKey`: same key plus
  non-overlapping Service ports.
- [x] Deterministic conflict winner when two bindings claim the same VIP.
- [x] `easylb.io/auto-assign: "false"` opt-out for a Service.

### L2 speaker

- [x] IPv4 ARP responder and gratuitous ARP on takeover.
- [x] IPv6 NDP responder: solicited-node multicast, Neighbor Solicitation
  replies, unsolicited Neighbor Advertisements, and DAD handling.
- [x] Per-VIP `Lease` and leader election with graceful release.
- [x] Eligibility checks: Ready node, Ready endpoint, local endpoint for
  `Local`, and interface/IP conflict protection (`VIPAlreadyAssigned`).
- [x] ARP and NDP run independently; one protocol failure does not stop the
  other.

### Gateway API

- [x] Optional Gateway API `v1` integration (`gatewayAPI.enabled`).
- [x] `Gateway` -> `VIPBinding` translation with ownerReference and cleanup.
- [x] Address sources: `Gateway.spec.addresses`, `easylb.io/vips`, and
  `easylb.io/vip-pool`.
- [x] Synthetic compatibility tests for Gateway API v1.0/v1.2/v1.4/v1.5/v1.7
  object shapes and unknown future fields.
- [x] Version/compatibility documentation, including the Kubernetes 1.31+
  requirement for Gateway API v1.5+.

### Observability

- [x] Prometheus metrics:
  - `easylb_controller_vip_allocations_total`
  - `easylb_controller_auto_bindings_total`
  - `easylb_speaker_advertised_vips`
  - `easylb_speaker_leader_changes_total`
  - `easylb_speaker_errors_total`
- [x] Controller and speaker metrics Services.
- [x] Optional Prometheus Operator `ServiceMonitor` in the chart.
- [x] Kubernetes events for allocation, conflicts, auto-binding lifecycle, and
  speaker failures.

### Helm and delivery

- [x] Helm chart with CRDs, controller, speaker, RBAC, PDB, and metrics.
- [x] Chart values documentation in `charts/easylb/README.md`.
- [x] OCI chart publishing workflow (`ghcr.io/<owner>/charts/easylb`).
- [x] Multi-arch image release workflow (`linux/amd64`, `linux/arm64`).
- [x] CI for gofmt, `go vet`, tests with `-race`, golangci-lint (revive,
  staticcheck, gosec, ...), standalone gosec, Helm lint/template, and image
  build.
- [x] Docs workflow for GitHub Pages (MkDocs Material).
- [x] k3d/k3s e2e workflow: auto-allocation, Service status, and binding GC.
- [x] Local k3d e2e passed (VIP allocated, published, and released).

### Documentation

- [x] README (EN/RU) with positioning as a ServiceLB + MetalLB L2 replacement.
- [x] Architecture, configuration, source IP, operations, troubleshooting,
  comparison, and Gateway API guides (EN/RU).
- [x] MetalLB migration guide (EN/RU).
- [x] Explicit-VIP-without-pool recipe for the keepalived use case.
- [x] Gateway API version compatibility and NGF/Envoy Gateway notes.
- [x] Uninstall and cleanup behavior, including auto-created bindings.
- [x] Architecture decision records.

## Next

### L2 parity and operations

- [ ] Per-pool advertisement policy: node selectors and interface selection,
  similar to MetalLB `L2Advertisement`.
- [ ] `loadBalancerSourceRanges` support or an explicit enforcement story.
- [ ] `ipMode: VIP` in `Service.status.loadBalancer.ingress`.
- [ ] Active duplicate-address detection before advertising an IPv4 L2 VIP.
- [ ] Graceful ARP/NDP withdrawal/docs for node drain and failover.
- [ ] Ready-made Prometheus alert rules and a dashboard.
- [ ] E2E for shared VIPs (`allow-shared-ip`) and IPv6 NDP on a real
  IPv6-enabled interface.
- [ ] E2E for failover: speaker pod deletion/node drain and Lease takeover.
- [ ] CRD upgrade strategy for `helm upgrade` (pre-upgrade hook or documented
  apply step).

### Migration and adoption

- [x] ServiceLB (klipper) migration guide, in addition to the MetalLB guide.
- [x] Examples: ingress-nginx, Envoy Gateway, NGINX Gateway Fabric.

## Not planned

These are intentionally outside the “small bare-metal, no BGP” scope:

- [ ] BGP, BFD, ECMP, and advanced route advertisement.
- [ ] DSR and L7/TLS termination in the load balancer.
- [ ] MetalLB advanced traffic policies and custom advertisement objects.
- [ ] Replacing kube-proxy or implementing a full dataplane.
- [ ] Automatic management of keepalived/VRRP.

## Known limitations

- one VIP has exactly one owner unless `allow-shared-ip`/`sharingKey` is used;
- `loadBalancerSourceRanges` is not enforced;
- per-pool node/interface advertisement is not implemented;
- the speaker requires `hostNetwork`, root, and `NET_RAW`;
- an l2 VIP does not answer ICMP echo;
- IPv6 NDP is implemented but still needs real-network e2e coverage;
- Helm does not upgrade CRDs from `crds/`;
- `mode: bgp` is reserved but not implemented.

## Related documentation

- [README](README.md)
- [Configuration](docs/en/configuration.md)
- [Operations and HA](docs/en/operations.md)
- [Gateway API](docs/en/gateway-api.md)
- [Migration from MetalLB](docs/en/migration-metallb.md)
