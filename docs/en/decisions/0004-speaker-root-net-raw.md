# 0004 — Speaker runs as root with NET_RAW

**Status:** Accepted

## Problem

The speaker opens raw sockets for ARP and ICMPv6 NDP. A non-root container with
`NET_RAW` can fail with `operation not permitted` because the effective
capability is not granted by the default runtime.

## Constraints

- The speaker needs raw ARP sockets and `hostNetwork`.
- It should not gain any other capabilities.
- The controller must remain non-root.

## Options

1. Non-root + `NET_RAW`.
2. Non-root + file capabilities (`cap_net_raw+ep`) on the binary.
3. Root + `NET_RAW`, dropping all other capabilities.

## Decision

Option 3 for the speaker. The controller stays non-root.

## Why

- Reliably works across container runtimes.
- The speaker is a privileged network component, similar to keepalived or
  MetalLB's speaker.
- All other capabilities are dropped; the container is not privileged.

## Consequences

- Pod Security Admission must allow the speaker (for example,
  `pod-security.kubernetes.io/enforce=privileged`).
- The speaker is a higher-value target; keep the image minimal and RBAC tight.

## Re-evaluation trigger

File capabilities become reliable in the target runtime, or PSA restrictions
make root unacceptable.
