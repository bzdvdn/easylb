# 0006 — Controller HA with leader election

**Status:** Accepted

## Problem

If the only controller pod is down, existing traffic keeps working but the
status stops converging. Recreated Services and node/endpoint changes are not
published until the controller returns.

## Constraints

- Small and medium clusters.
- No external database.
- Status writes must not race between replicas.
- Existing traffic must not depend on controller availability.

## Options

1. One replica; accept a status-convergence gap.
2. Two or three replicas with leader election and standby.
3. Active-active sharding by Service.

## Decision

Option 2: 2 replicas, leader election, PDB, node spreading, and optimistic lock
on status patches.

## Why

- Simple and sufficient for small/medium clusters.
- A standby can take over without changing the dataplane.
- Optimistic locking prevents a stale leader from overwriting a newer status.
- PDB and spreading survive pod and node failures.
- No sharding complexity or consistent-hash ownership.

## Consequences

- Only one replica reconciles at a time.
- Failover takes up to `leaseDuration` (15s) after a crash, faster on graceful
  shutdown.
- Not designed for very large Service counts; revisit sharding if needed.

## Re-evaluation trigger

Controller reconcile latency or RTO requirements exceed what a single active
leader can provide.
