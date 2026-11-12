# 0005 — An l2 VIP is not assigned to the interface

**Status:** Accepted

## Problem

For an `l2` VIP the speaker advertises the address. A natural question is
whether the VIP should be assigned to the node interface, which would make the
kernel answer ARP and ICMP and would allow `ping`.

## Constraints

- A crashed or partitioned speaker must not leave a stale VIP answering on the
  wrong node.
- The speaker must coexist with keepalived for other VIPs.
- TCP/UDP dataplane already works through kube-proxy.

## Options

1. Assign the VIP to the interface on the leader; remove it on loss.
2. Keep the VIP unassigned; answer ARP from userspace.
3. Add a userspace ICMP responder for the VIP.

## Decision

Option 2. The VIP is not assigned to the interface and ICMP is not answered.

## Why

- No stale address if the speaker crashes or the node is partitioned.
- ARP ownership is explicit and tied to the Lease.
- No conflict with the kernel's ARP for keepalived VIPs.
- ICMP echo replies with a source IP that is not assigned are difficult to send
  correctly without building full IP packets.

## Consequences

- `ping` to an l2 VIP does not work; use ARP/TCP checks.
- Some monitoring systems that rely on ICMP need an alternative.

## Re-evaluation trigger

A hard requirement for ICMP monitoring, or a safe interface-address handoff
mechanism with crash cleanup.
