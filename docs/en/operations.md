# Operations and HA

**English** | [Русский](../ru/operations.md)

## Install, upgrade, uninstall

```bash
# install
helm upgrade --install easylb charts/easylb \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml

# upgrade
helm upgrade easylb charts/easylb \
  --namespace easylb-system \
  -f my-values.yaml

# uninstall (CRDs stay)
helm uninstall easylb -n easylb-system
```

Notes:

- CRDs from `charts/easylb/crds/` are installed on the first `helm install` and
  are **not** updated by `helm upgrade`. Apply `deploy/crds.yaml` separately
  when the schema changes.
- `helm uninstall` does not remove CRDs. Removing a CRD deletes all
  `VIPPool`/`VIPBinding` objects.
- If you previously installed the static `deploy/` manifests, delete them before
  using the chart to avoid ownership conflicts.

### What uninstall cleans up

easyLB never installs iptables/ipvs rules itself: kube-proxy derives them from
`Service.status.loadBalancer.ingress`. Keep that in mind when removing the
release.

`helm uninstall easylb -n easylb-system` removes:

- the controller Deployment and the speaker DaemonSet;
- ServiceAccounts, ClusterRoles/ClusterRoleBindings, and the PDB;
- `VIPPool`/`VIPBinding` objects rendered from the chart's `pools`/`bindings`
  values;
- the release's controller leader-election Lease (released on graceful
  shutdown).

It does **not** remove:

- CRDs from `charts/easylb/crds/`;
- `VIPPool`/`VIPBinding` objects created outside the release;
- the `easylb-system` namespace (including `--create-namespace`);
- `Service.status.loadBalancer.ingress` entries already published;
- runtime Leases that are not deleted by a graceful shutdown;
- iptables/ipvs rules — those belong to kube-proxy.

What happens to traffic after uninstall:

- `mode: l2` VIPs stop answering ARP as soon as the speaker pods terminate. The
  VIP in Service status and kube-proxy rules remain, so TCP may still work for a
  short time on nodes with stale neighbour caches, then the VIP becomes
  unreachable.
- `mode: external` VIPs keep working for as long as keepalived owns the address
  and the VIP remains in Service status: kube-proxy keeps the DNAT rules even
  without easyLB. This is a stale configuration, not a supported steady state.
- Node IPs published by the controller also stay in status.

Full cleanup after uninstall:

```bash
# Remove the objects that are not part of the Helm release.
kubectl get vipbinding -A
kubectl get vippool
kubectl delete vipbinding <name> -n <namespace>
kubectl delete vippool <name>

# Clear published addresses if the Services are not being deleted.
kubectl -n <namespace> patch svc <service> --type merge \
  -p '{"status":{"loadBalancer":{"ingress":[]}}}' --subresource=status

# Remove stale runtime Leases if the namespace is kept.
kubectl -n easylb-system get lease
kubectl -n easylb-system delete lease <name>

# Optional, destructive: deletes all VIPPool/VIPBinding objects.
kubectl delete crd vippools.easylb.io vipbindings.easylb.io
```

Deleting the `VIPBinding` is the supported way to make the speaker stop
advertising and delete its per-VIP Lease. There is no finalizer or cleanup Job
that patches Service status on uninstall.

## Controller HA

The chart runs 2 replicas with:

- leader election (`Lease easylb.io` in `easylb-system`);
- `LeaderElectionReleaseOnCancel: true`;
- PDB `minAvailable: 1`;
- topology spread by `kubernetes.io/hostname`;
- rolling update `maxUnavailable: 0`, `maxSurge: 1`;
- optimistic locking on `status.loadBalancer.ingress` and `VIPBinding.status`.

Failure behavior:

| Event | Effect |
|---|---|
| Leader pod dies | standby acquires the Lease; status does not change |
| Graceful leader shutdown | Lease released immediately |
| Hard crash | Lease expires after `leaseDuration` (15s) |
| All controllers down | traffic keeps working; status is stale until they return |
| API/etcd outage | no reconciliation; status is not cleared |

## Speaker HA

The speaker is a DaemonSet, one pod per node. For each `mode: l2` VIPBinding it
runs a leader election through a per-VIP `Lease` in `easylb-system`.

- Graceful shutdown releases the Lease immediately (process context is tied to
  the signal handler).
- A crash is detected after `leaseDuration` (15s by default).
- The new leader sends gratuitous ARP.
- The VIP is not assigned to the interface, so a crashed old leader cannot leave
  a stale address behind.

On failover, established TCP sessions break; clients must reconnect. This is a
property of any L4 VIP.

## Monitoring

Controller (controller-runtime metrics on `:8080`):

```promql
leader_election_master_status{name="easylb.io"}
controller_runtime_reconcile_errors_total
controller_runtime_reconcile_total
controller_runtime_reconcile_time_seconds
```

Speaker (controller-runtime metrics on `:8082`):

- `controller_runtime_reconcile_*` for the `vipbinding` controller;
- logs: `advertising VIPs`, `stopped leading`, `failed to start ARP responder`,
  `VIPAlreadyAssigned`;
- Leases in `easylb-system` show the current holder;
- with kube-state-metrics, `kube_lease_owner` / `kube_lease_renew_time` are
  useful for Lease age.

Useful alerts:

- `leader_election_master_status{name="easylb.io"} != 1` for 30s;
- `rate(controller_runtime_reconcile_errors_total[5m]) > 0.1`;
- speaker DaemonSet not fully ready:
  `kube_daemonset_status_number_ready{daemonset="easylb-speaker"} < kube_daemonset_status_desired_number_scheduled`;
- a VIPBinding stuck without `Ready=True` (kube-state-metrics or a custom
  exporter);
- Lease not renewed for longer than `leaseDuration` (kube-state-metrics).

Also watch node conntrack:

```bash
cat /proc/sys/net/netfilter/nf_conntrack_count
cat /proc/sys/net/netfilter/nf_conntrack_max
```

## Security

- Controller: non-root, read-only root filesystem, drops all capabilities.
- Speaker: `hostNetwork`, root, `NET_RAW`; required for `AF_PACKET`/ARP. It
  drops all other capabilities.
- With Pod Security Admission, the `easylb-system` namespace must allow the
  speaker, for example:

  ```bash
  kubectl label namespace easylb-system \
    pod-security.kubernetes.io/enforce=privileged --overwrite
  ```

- RBAC is least-privilege: Services, Nodes, EndpointSlices, VIPBindings,
  VIPPools, Leases, Events, and Gateways (only when Gateway integration is
  enabled).
- The controller can create/update/delete `VIPBinding` objects because of the
  optional Gateway integration. Restrict who can create Gateways in namespaces
  where this feature is enabled.
- Validate VIPs and pools before applying; `VIPBinding` conditions expose
  `Conflict`, `PoolExhausted`, `PoolNotFound`, `InvalidSpec`, `InvalidVIP`.

## Rollout and rollback

1. Build and push a new image tag.
2. `helm upgrade` with the new tag.
3. For the controller, `maxUnavailable: 0` keeps a leader during rollout.
4. For the speaker, `maxUnavailable: 1` updates one node at a time. If the
   leader pod is updated, the VIP may move; established sessions may break.
5. Verify:

   ```bash
   kubectl -n easylb-system rollout status deploy/easylb-controller
   kubectl -n easylb-system rollout status ds/easylb-speaker
   kubectl get vipbinding -A
   ```

6. Roll back with `helm rollback`; if a CRD changed, apply the previous CRD
   separately.

## Capacity

- Controller: 2 small replicas (requests 20m CPU / 32Mi, limits 200m / 128Mi).
- Speaker: tiny per-node pod (10m / 24Mi requests).
- A `VIPPool` expands to at most 4096 addresses.
- The IP allocator is single-writer (`MaxConcurrentReconciles: 1`); this is
  fine for small and medium clusters.
- kube-proxy/conntrack are the bottleneck for large numbers of long-lived
  sessions, not easyLB.

## Runbook

### Controller leader failover

```bash
kubectl -n easylb-system get lease easylb.io -o jsonpath='{.spec.holderIdentity}{"\n"}'
kubectl -n easylb-system get pods -o wide
```

If no leader appears within ~30s, check controller logs and that the
`easylb-system` namespace and Lease RBAC exist.

### Speaker / VIP failover

```bash
kubectl -n easylb-system get lease -o wide
kubectl -n easylb-system logs ds/easylb-speaker --tail=200
arping -c 3 <VIP>
```

If the Lease holder is a pod that no longer exists and the Lease is older than
`leaseDuration`, delete the Lease; a current speaker will recreate it.

### Keepalived failover

Keepalived is outside easyLB. Check VRRP state, VIP ownership, and the
`healthCheckNodePort` script. The VIP must be on a node with a local ingress
endpoint for `Local`.

### Node failure

- Controller: the standby on another node continues.
- Speaker: another node acquires the Lease and sends gratuitous ARP.
- Node IPs: the controller removes NotReady node addresses from Service status
  when it reconciles.
- DNS: DNS to node IPs does not know about the failure; use a VIP for HA.
