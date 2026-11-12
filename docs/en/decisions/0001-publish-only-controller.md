# 0001 — Publish-only controller

**Status:** Accepted

## Problem

Keepalived VIP traffic that goes through k3s ServiceLB (klipper) is
DNATed and masqueraded inside the `svclb` pod. The ingress controller sees the
`svclb` pod IP instead of the client IP. Node IPs work because kube-proxy
intercepts them before klipper.

## Constraints

- kube-proxy is present on every node and already handles
  `status.loadBalancer.ingress`.
- The project should stay small, safe, and easy to install.
- Keepalived remains the VIP owner for existing deployments.

## Options

1. Fork klipper and change its iptables rules.
2. Write a custom dataplane (ARP/NDP or a proxy).
3. Publish addresses into Service status and let kube-proxy do the dataplane.

## Decision

Option 3: a publish-only controller.

## Why

- Fixes the root cause: kube-proxy did not know the VIP.
- No extra hop, no pod in the packet path, no `NET_ADMIN` in the controller.
- Existing traffic path and conntrack semantics stay standard.
- Keepalived continues to own VIPs; easyLB only publishes them.

## Consequences

- No hostPort/NAT fallback for public clouds.
- No `loadBalancerSourceRanges` enforcement.
- All dataplane behavior (timeouts, conntrack, long sessions) is kube-proxy's.

## Re-evaluation trigger

Need for public-cloud/NAT fallback, source-range enforcement, DSR, or full
independence from kube-proxy.
