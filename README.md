# easyLB

**English** | [Русский](README.ru.md)

A lightweight Kubernetes LoadBalancer controller for bare-metal, on-prem,
k3s and RKE2 clusters. It publishes node IPs and VIPs into
`Service.status.loadBalancer.ingress` and leaves the entire dataplane to the
built-in **kube-proxy** — no extra pod, NAT hop, or packet proxying.

It is a drop-in replacement for the k3s/RKE2 ServiceLB and for MetalLB's L2
mode in small bare-metal clusters that do not need BGP or MetalLB's advanced
traffic-engineering features.

You can bring VIPs you already own (keepalived/VRRP, a hardware load balancer,
another controller) and let easyLB publish them, or let easyLB provide a
complete HA load balancer itself through its L2 speaker, ARP and per-VIP leader
election.

## Why easyLB

Kubernetes on-prem usually has no cloud load balancer. The built-in k3s/RKE2
ServiceLB (klipper) fills the gap with `svclb` pods using `hostPort` and
DNAT+MASQUERADE. That adds a hop and can hide the client source IP.

easyLB takes the control-plane route: it writes the desired addresses into the
Service status, and kube-proxy programs the DNAT rules:

```text
client -> NodeIP or VIP -> kube-proxy -> ingress-nginx / pods
                              ^
                 KUBE-EXT rules for addresses from status
```

With `externalTrafficPolicy: Local` and a local Ready endpoint, the client
source IP is preserved, and no `svclb` pod or extra NAT hop is involved. The
same Service can use an existing VIP or an easyLB-managed L2 VIP.

## Who it is for

easyLB targets small and medium **bare-metal / on-prem** clusters where:

- Kubernetes has no cloud load balancer, but `Service` type `LoadBalancer` is
  still needed;
- DNS points at node IPs and/or stable VIPs;
- you already have VIPs (keepalived/VRRP, hardware LB) and want Kubernetes to
  use them without an extra proxy;
- or you want a complete HA L2 VIP without BGP, MetalLB, or keepalived;
- ingress-nginx or a Gateway API data plane runs as a Service/DaemonSet;
- client source IP matters (`externalTrafficPolicy: Local`).

easyLB is **not** intended for:

- public-cloud/NAT scenarios that need a hostPort fallback;
- `loadBalancerSourceRanges` enforcement;
- BGP/ECMP, DSR, L7/TLS termination;
- MetalLB features beyond L2 (BGP, custom advertisements, advanced traffic
  policies);
- automatic management of keepalived/VRRP.

## Features

- Drop-in replacement for k3s/RKE2 ServiceLB: no `svclb` pods, no hostPort,
  no extra NAT hop.
- Replaces MetalLB L2 mode for small bare-metal clusters: `VIPPool` IPAM, ARP
  advertisement, per-VIP leader election, and failover without BGP.
- Publishes node IPs and VIPs into `status.loadBalancer.ingress`.
- Bring-your-own-VIP mode (`external`) for keepalived/VRRP, hardware LBs, or
  any externally managed address.
- Native L2 mode (`l2`): easyLB owns the VIP, advertises it with ARP and
  provides leader election and failover through Kubernetes Leases.
- Automatic VIP allocation from `VIPPool`, or explicit addresses in
  `VIPBinding`.
- HA controller: multiple replicas, leader election, PDB, node spreading,
  optimistic locking on status updates.
- Source IP preservation with `externalTrafficPolicy: Local`.
- CRD-only configuration; no annotations on ingress/gateway Services.
- Optional native Gateway API integration: a `Gateway` becomes a `VIPBinding`.
- Helm chart, published as an OCI artifact.

## VIP modes

| Mode | VIP owner | Speaker | ICMP ping | Use case |
|---|---|---|---|---|
| `external` | keepalived/VRRP, hardware LB, or admin | no | yes, if assigned | integrate VIPs you already own; easyLB only publishes status |
| `l2` | easyLB speaker | yes | no (ARP only) | complete HA LB without keepalived, BGP, or MetalLB |
| `bgp` | reserved | future | — | future BGP mode |

Both modes can be mixed on one Service: create one binding for an existing
keepalived VIP and another for an easyLB-managed L2 VIP.

For `l2`, the VIP is **not assigned to the interface**: the speaker answers
ARP, and kube-proxy handles TCP/HTTP. `ping` therefore does not work for such
a VIP — this is expected (the same as MetalLB L2). Use TCP checks instead.

## Quick start

### 1. Image

```bash
docker build -t <registry>/easylb:<tag> .
docker push <registry>/easylb:<tag>
```

### 2. Helm

```bash
helm upgrade --install easylb charts/easylb \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml
```

The released chart is also published as an OCI artifact:

```bash
helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml
```

Minimal `my-values.yaml`:

```yaml
controller:
  image:
    repository: <registry>/easylb
    tag: <tag>
  publishNodeIPs: true      # false if DNS only uses VIPs

speaker:
  enabled: true
  image:
    repository: <registry>/easylb
    tag: <tag>

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
  # Optional: publish a VIP you already own (keepalived/VRRP, hardware LB).
  ingress-external:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
```

A ready-to-use example is in `charts/easylb/values-example.yaml`.

### 3. Verification

```bash
kubectl -n easylb-system get deploy,ds,pods
kubectl get vippool
kubectl get vipbinding -A
kubectl -n easylb-system get lease
kubectl -n ingress-nginx get svc my-release-ingress-nginx-controller -o wide
```

The VIPs should appear in `EXTERNAL-IP`. For `l2`, verify connectivity over
TCP:

```bash
arping -c 3 192.168.56.240
curl -k -H 'Host: echo1.local' https://192.168.56.240/
```

## Configuration

CRDs are the source of truth:

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPPool
metadata:
  name: ingress-vips
spec:
  addresses:
    - 192.168.56.240-192.168.56.249
  autoAssign: true
---
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

Service annotations (`easylb.io/vips`, `easylb.io/vip-config`) are no longer
used. See [docs/en/configuration.md](docs/en/configuration.md).

## Source IP

In short:

- `externalTrafficPolicy: Local` + a local Ready endpoint + kube-proxy
  preserves the client source IP.
- `Cluster` may SNAT traffic forwarded to another node; source IP is not
  guaranteed.
- For `l2`, the speaker only elects a leader on a node with a local endpoint
  (when `Local`), so the VIP lands where an ingress pod exists.
- For `external` VIPs, the external owner (keepalived, a hardware LB, or
  another system) is responsible for health and failover; the VIP owner must
  have a local ingress pod.

Details: [docs/en/source-ip.md](docs/en/source-ip.md).

## Gateway API

Gateway API implementations (Envoy Gateway, NGINX Gateway Fabric, Cilium,
Traefik, Kong, Istio, ...) expose their data plane through a `Service` of type
`LoadBalancer`. easyLB manages that Service exactly like ingress-nginx. With
`gatewayAPI.enabled: true`, easyLB also watches `Gateway` objects in the stable
`gateway.networking.k8s.io/v1` API and creates the `VIPBinding` automatically
from `Gateway.spec.addresses` and a few annotations.

easyLB does not depend on a specific implementation or Gateway API release: it
uses unstructured objects and reads only the stable `v1` surface, so newer
fields and resources are ignored instead of breaking reconciliation. Gateway
API v1.5+ requires Kubernetes 1.31+; on older clusters use v1.4.1 (or v1.0.0
for Kubernetes 1.25) as long as the CRDs serve the `v1` API. easyLB does not
implement ListenerSet, TLSRoute, gateway status handling, or other new Gateway
API features.

See [docs/en/gateway-api.md](docs/en/gateway-api.md).

## High availability

- Controller: 2 replicas, leader election, PDB, node spreading. Losing the
  leader does not stop traffic; a standby takes the Lease.
- Speaker: one Lease per `VIPBinding`, ARP/gratuitous ARP. A graceful shutdown
  releases the Lease immediately; after a crash the Lease expires after
  `leaseDuration` (15s).
- kube-proxy/conntrack remain the dataplane. Established TCP sessions break
  when the VIP owner or the backend pod changes — a property of any L4 VIP, not
  of easyLB.
- If DNS uses node IPs, keep `publishNodeIPs: true`; with `false` only VIPs are
  published.

## Limitations

- no `loadBalancerSourceRanges`;
- no hostPort/NAT fallback for public clouds;
- IPv6 for `l2`/NDP is not implemented yet (the controller can publish IPv6,
  but the speaker skips it);
- `mode: bgp` is reserved but not implemented;
- CRDs from Helm `crds/` are not updated by `helm upgrade`;
- the speaker requires `hostNetwork`, root and `NET_RAW`; with Pod Security
  Admission the namespace must allow this (for example,
  `pod-security.kubernetes.io/enforce=privileged`);
- an l2 VIP does not answer ICMP;
- easyLB does not detect a conflict with an external VIP owner. One VIP must
  have exactly one owner; this is a configuration responsibility.

## Documentation

Rendered docs: <https://bzdvdn.github.io/easylb/>

English:

- [Helm chart](charts/easylb/README.md)
- [Architecture](docs/en/architecture.md)
- [Configuration](docs/en/configuration.md)
- [Source IP](docs/en/source-ip.md)
- [Gateway API](docs/en/gateway-api.md)
- [Operations and HA](docs/en/operations.md)
- [Troubleshooting](docs/en/troubleshooting.md)
- [Comparison with ServiceLB/MetalLB/kube-vip](docs/en/comparison.md)
- [Architecture decisions](docs/en/decisions/)

Russian:

- [README](README.ru.md)
- [Архитектура](docs/ru/architecture.md)
- [Конфигурация](docs/ru/configuration.md)
- [Source IP](docs/ru/source-ip.md)
- [Gateway API](docs/ru/gateway-api.md)
- [Эксплуатация и HA](docs/ru/operations.md)
- [Troubleshooting](docs/ru/troubleshooting.md)
- [Сравнение](docs/ru/comparison.md)
- [Архитектурные решения](docs/ru/decisions/)

## Development

```bash
make build          # controller and speaker binaries
make test
make vet
make lint           # golangci-lint (revive, staticcheck, gosec, ...)
make sec            # standalone gosec scan
make docker-build IMAGE=<registry>/easylb:<tag>
make helm-lint
make helm-template
```

Layout:

```text
cmd/controller/        easyLB controller
cmd/speaker/           L2 speaker
api/v1alpha1/          VIPPool, VIPBinding
internal/controller/   Service and VIPBinding controllers
internal/speaker/      Lease, ARP, eligibility, interface selection
internal/ipam/         pool parsing and allocation
internal/vip/          VIP model and VIPBinding source
deploy/                static manifests and CRDs
charts/easylb/         Helm chart
```

## Roadmap

- IPv6 NDP for the speaker.
- BGP/ECMP speaker (`mode: bgp`).
- Metrics/ServiceMonitor and ready-made alerts.
- Optional `loadBalancerSourceRanges` enforcement.
- Better conflict diagnostics between external VIP owners and easyLB.
