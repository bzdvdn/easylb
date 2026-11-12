# Architecture

**English** | [Русский](../ru/architecture.md)

easyLB is a control-plane controller plus an optional per-node L2 speaker. It
does not proxy packets: DNAT and load balancing are performed by kube-proxy,
exactly as for any Kubernetes Service.

## Components

```text
                    Kubernetes API
                         │
        ┌────────────────┴────────────────┐
        │                                 │
  easyLB controller                 easyLB speaker
  (Deployment, 2 replicas)           (DaemonSet, hostNetwork)
        │                                 │
        │ VIPPool / VIPBinding             │ Lease per VIPBinding
        │ status.loadBalancer.ingress      │ ARP/NDP + gratuitous ARP
        ▼                                 ▼
   kube-proxy on all nodes        VIP (mode: l2)
        │
        ▼
   ingress-nginx / pods
```

### Controller

A Deployment in `easylb-system` with 2 replicas and leader election through the
`easylb.io` Lease. Only the leader is active.

Watches:

- `Service` (type LoadBalancer);
- `Node` (addresses and readiness);
- `EndpointSlice` (local Ready endpoints for
  `externalTrafficPolicy: Local`);
- `VIPBinding` and `VIPPool`.

Does:

- allocates a VIP from a pool (when `poolRef` is used) and writes it to
  `VIPBinding.status.allocatedIPs`;
- assembles addresses for the Service: node IPs (when `publishNodeIPs` is
  enabled) plus VIPs from bindings;
- patches `status.loadBalancer.ingress` with optimistic locking;
- updates `VIPBinding` conditions;
- when automatic allocation is enabled, creates `auto-<service>` bindings for
  matching Services and cleans them up through owner references;
- never changes the Service `spec` and never touches external VIP owners.

Node IP selection:

- `externalTrafficPolicy: Local` — only Ready nodes with a Ready endpoint;
- `externalTrafficPolicy: Cluster` — all Ready nodes;
- address type is configured with `--node-ip-types` (default `InternalIP`).

### Speaker

Optional (`speaker.enabled`). A DaemonSet on every node, `hostNetwork`, root +
`NET_RAW`, because it opens raw sockets for ARP and IPv6 NDP.

Watches `VIPBinding`, `Node`, `Service`, `EndpointSlice`. It processes only
`mode: l2` and completely ignores `mode: external`.

For each `VIPBinding`:

1. Checks eligibility:
   - the node is Ready;
   - there is at least one Ready endpoint;
   - with `Local`, there is a Ready endpoint on this node.
2. Selects an interface:
   - `--interface` when set;
   - otherwise the interface that owns the node's `InternalIP`/`ExternalIP`.
3. Checks that the VIP is not assigned to the interface. If it is, the speaker
   refuses to advertise it and emits a `VIPAlreadyAssigned` event (protection
   against a keepalived conflict).
4. Participates in leader election through a `coordination.k8s.io/Lease`
   (one Lease per `VIPBinding`).
5. The leader starts the ARP responder for IPv4 and the NDP responder for
   IPv6: they answer ARP/neighbor solicitations for the VIP and send
   gratuitous ARP/unsolicited neighbor advertisements on takeover.
6. On loss of the Lease, loss of eligibility, or binding deletion, it stops the
   responder.

The VIP is **not assigned to the interface** and is not added to `ip addr`. The
kernel answers neither ARP/NDP nor ICMP for such an address: ARP/NDP is
handled by the speaker, and ICMP echo is not answered at all. TCP/UDP is
handled by kube-proxy via `KUBE-EXT` rules.

### Gateway API

Gateway API implementations expose a data-plane `Service` of type
`LoadBalancer`. easyLB manages that Service like ingress-nginx. With the
optional `--enable-gateway-api` integration, a `Gateway` can also be translated
into a `VIPBinding`; see [gateway-api.md](gateway-api.md).

### Dataplane

kube-proxy on every node:

- reads `status.loadBalancer.ingress`;
- installs `KUBE-EXT` rules for NodeIP/VIP;
- DNATs to an endpoint;
- maintains conntrack.

easyLB adds no netfilter rules and is not on the packet path. Therefore:

- restarting the controller or speaker does not break established connections;
- long TCP/stream sessions live according to conntrack/kube-proxy timeouts;
- when the VIP owner node or the backend pod fails, established sessions break
  — a property of any L4 VIP, not of easyLB.

### Keepalived

In `external` mode, keepalived remains the VIP owner:

- assigns the VIP to an interface;
- performs VRRP/ARP;
- moves the VIP between nodes.

easyLB only publishes the VIP in the Service status. The VIP owner node must
have a local ingress endpoint; otherwise `Local` traffic is dropped. A
`vrrp_script` checking the Service's `healthCheckNodePort` is recommended (see
[source-ip.md](source-ip.md)).

## Traffic flows

### NodeIP

```text
DNS -> NodeIP:443
       -> kube-proxy KUBE-EXT (NodeIP is in status)
       -> local ingress pod (Local)
       -> client source IP
```

### External VIP (keepalived)

```text
DNS -> VIP:443
       -> keepalived: VIP is assigned to the node, the kernel answers ARP
       -> kube-proxy KUBE-EXT
       -> local ingress pod
       -> client source IP
```

### L2 VIP (easyLB speaker)

```text
DNS -> VIP:443
       -> speaker: userspace ARP, VIP is not on the interface
       -> kube-proxy KUBE-EXT
       -> local ingress pod
       -> client source IP

ICMP ping -> no reply (expected)
```

## VIP ownership model

One VIP has exactly one owner:

| Mode | Owner | Configured in |
|---|---|---|
| `external` | keepalived/administrator | keepalived + `VIPBinding(mode: external)` |
| `l2` | easyLB speaker | `VIPBinding(mode: l2)` |
| `bgp` | future easyLB speaker | reserved |

The same VIP must not be held by both keepalived and `mode: l2`. The speaker
only checks the local interface on the leader node, so cross-node conflicts must
be avoided by configuration.

## High availability

### Controller

- 2 replicas, leader election (`Lease easylb.io`, `easylb-system`);
- PDB `minAvailable: 1`, spreading by `kubernetes.io/hostname`;
- rolling update `maxUnavailable: 0`, `maxSurge: 1`;
- `LeaderElectionReleaseOnCancel: true`;
- optimistic locking on status patches.

When the leader dies, a standby takes the Lease and the status does not change.
Existing traffic does not depend on the controller.

### Speaker

- one Lease per `VIPBinding`;
- `ReleaseOnCancel: true` and a process-lifetime context: on SIGTERM the Lease
  is released immediately;
- after a crash the Lease expires after `leaseDuration` (15s by default);
- the new leader sends gratuitous ARP.

On VIP failover, existing TCP sessions break; new connections go to the new
owner.

### Dataplane

kube-proxy and conntrack are standard k3s components. Their availability is the
cluster's responsibility, not easyLB's.

## Namespaces, RBAC, CRDs

- `easylb-system` — controller, speaker, Leases.
- `VIPPool` — cluster-scoped.
- `VIPBinding` — namespaced, usually in the Service namespace.
- Controller RBAC: services/status, nodes, endpointslices, events, leases,
  vipbindings/status, vippools, and Gateways (optional Gateway integration).
- Speaker RBAC: services, nodes, endpointslices, vipbindings, leases, events.

## Architecture decisions

- [0001 — publish-only controller](decisions/0001-publish-only-controller.md)
- [0002 — CRDs as the single source of truth](decisions/0002-crd-as-source-of-truth.md)
- [0003 — L2 speaker with Leases](decisions/0003-l2-speaker-with-leases.md)
- [0004 — speaker root + NET_RAW](decisions/0004-speaker-root-net-raw.md)
- [0005 — an l2 VIP is not assigned to the interface](decisions/0005-l2-vip-not-assigned.md)
- [0006 — controller HA](decisions/0006-controller-ha.md)
