# 0003 — L2 speaker with per-VIP Leases

**Status:** Accepted

## Problem

Some clusters do not run keepalived but still need VIPs that are advertised on
the LAN. The VIP must move between nodes when the owner fails.

## Constraints

- No extra control-plane database.
- Kubernetes API is already a dependency.
- Failover must not leave a stale VIP on an old node.
- Small and medium clusters, not thousands of VIPs.

## Options

1. Implement memberlist-based leader election (MetalLB-like).
2. Use Kubernetes `coordination.k8s.io/Lease` per VIP.
3. Depend on keepalived/VRRP for all VIPs.

## Decision

Option 2: one Lease per `VIPBinding`, plus userspace ARP/NDP responders. The VIP is
not assigned to the interface.

## Why

- Reuses the API server and existing RBAC.
- No new membership protocol to operate.
- A Lease holder is observable with `kubectl get lease`.
- `ReleaseOnCancel` gives fast graceful failover; a crash expires after
  `leaseDuration`.
- Not assigning the VIP avoids stale addresses on a crashed node.

## Consequences

- Failover depends on API availability.
- ARP responder requires `AF_PACKET`/`NET_RAW`.
- One VIP per Lease; many VIPs mean many small Leases.

## Re-evaluation trigger

API outage tolerance requirements, very large VIP counts, or the need for
memberlist-style failure detection.
