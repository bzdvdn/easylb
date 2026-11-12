![easyLB](assets/easylb-banner.svg)

# easyLB

A lightweight Kubernetes LoadBalancer controller for bare-metal, on-prem,
k3s and RKE2 clusters. easyLB publishes node IPs and VIPs into
`Service.status.loadBalancer.ingress` and leaves the entire dataplane to the
built-in **kube-proxy**.

It is a drop-in replacement for the k3s/RKE2 ServiceLB and for MetalLB's L2
mode in small bare-metal clusters that do not need BGP or MetalLB's advanced
traffic-engineering features. You can bring VIPs you already own
(keepalived/VRRP, a hardware load balancer) or let easyLB own them through its
L2 speaker, ARP and per-VIP leader election.

[Русская документация](ru/architecture.md)

## Why easyLB

Kubernetes on-prem usually has no cloud load balancer. The built-in k3s/RKE2
ServiceLB (klipper) fills the gap with `svclb` pods using `hostPort` and
DNAT+MASQUERADE. That adds a hop and can hide the client source IP.

easyLB is a control-plane controller only:

```text
client -> NodeIP or VIP -> kube-proxy -> ingress-nginx / pods
                              ^
                 KUBE-EXT rules for addresses from status
```

Once a VIP is published in the Service status, kube-proxy installs DNAT rules
for it. With `externalTrafficPolicy: Local` and a local endpoint, the client
source IP is preserved, and no `svclb` pods or extra NAT hop are needed.

## Features

- Drop-in replacement for k3s/RKE2 ServiceLB: no `svclb` pods, hostPort, or
  extra NAT hop.
- Replaces MetalLB L2 mode for small clusters: `VIPPool` IPAM, ARP
  advertisement, per-VIP leader election, and failover without BGP.
- MetalLB-style automatic allocation: matching `LoadBalancer` Services get a
  `VIPBinding` and VIP without creating CRs by hand.
- Publishes node IPs and VIPs into `status.loadBalancer.ingress`.
- Bring-your-own-VIP mode (`external`) for keepalived/VRRP, hardware LBs, or
  any externally managed address.
- Native L2 mode (`l2`): easyLB owns the VIP and advertises IPv4 with ARP and
  IPv6 with NDP.
- Source IP preservation with `externalTrafficPolicy: Local`.
- HA controller: multiple replicas, leader election, PDB, node spreading.
- Prometheus metrics and an optional ServiceMonitor.
- CRD-only configuration; no annotations on ingress/gateway Services.
- Optional native Gateway API integration: a `Gateway` becomes a `VIPBinding`.
- Helm chart, published as an OCI artifact.

## Install

```bash
helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml
```

A minimal `my-values.yaml`:

```yaml
controller:
  publishNodeIPs: false

speaker:
  enabled: true

pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true

bindings:
  ingress-easylb:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: l2
    poolRef: ingress-vips
```

Omit `bindings` to let a pool with `autoAssign: true` create `auto-<service>`
bindings automatically, like MetalLB.

## Gateway API

easyLB works with any Gateway API implementation that exposes its data plane
through a `Service` of type `LoadBalancer`. With `gatewayAPI.enabled: true` it
watches `Gateway` objects in the stable `gateway.networking.k8s.io/v1` API and
creates `VIPBinding` resources automatically.

easyLB uses unstructured objects and reads only the stable `v1` surface, so
newer Gateway API fields and resources are ignored instead of breaking
reconciliation. Gateway API v1.5+ requires Kubernetes 1.31+; on older clusters
use v1.4.1 (or v1.0.0 for Kubernetes 1.25) as long as the CRDs serve `v1`.
ListenerSet, TLSRoute, gateway status handling, and other new Gateway API
features are not implemented by easyLB.

See [Gateway API](en/gateway-api.md).

## Documentation

English:

- [Architecture](en/architecture.md)
- [Configuration](en/configuration.md)
- [Source IP](en/source-ip.md)
- [Gateway API](en/gateway-api.md)
- [Operations and HA](en/operations.md)
- [Troubleshooting](en/troubleshooting.md)
- [Comparison](en/comparison.md)
- [Migrating from MetalLB](en/migration-metallb.md)
- [Migrating from ServiceLB](en/migration-servicelb.md)
- [Roadmap](roadmap.md)
- [Community](community.md)
- [Examples](https://github.com/bzdvdn/easylb/tree/main/examples)
- [Architecture decisions](en/decisions/0001-publish-only-controller.md)

Russian:

- [Архитектура](ru/architecture.md)
- [Конфигурация](ru/configuration.md)
- [Source IP](ru/source-ip.md)
- [Gateway API](ru/gateway-api.md)
- [Эксплуатация и HA](ru/operations.md)
- [Troubleshooting](ru/troubleshooting.md)
- [Сравнение](ru/comparison.md)
- [Миграция с MetalLB](ru/migration-metallb.md)
- [Миграция с ServiceLB](ru/migration-servicelb.md)
- [Roadmap](roadmap.md)
- [Архитектурные решения](ru/decisions/0001-publish-only-controller.md)

## Limitations

- no `loadBalancerSourceRanges`;
- no hostPort/NAT fallback for public clouds;
- no BGP/ECMP or MetalLB features beyond L2;
- `mode: bgp` is reserved but not implemented;
- CRDs from Helm `crds/` are not updated by `helm upgrade`;
- the speaker requires `hostNetwork`, root and `NET_RAW`;
- an l2 VIP does not answer ICMP;
- one VIP must have exactly one owner.
