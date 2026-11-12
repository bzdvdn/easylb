# Troubleshooting

**English** | [Русский](../ru/troubleshooting.md)

## First checks

```bash
kubectl -n easylb-system get deploy,ds,pods -o wide
kubectl get vippool
kubectl get vipbinding -A -o wide
kubectl get vipbinding -A -o yaml
kubectl -n ingress-nginx get svc my-release-ingress-nginx-controller -o wide
kubectl -n easylb-system get lease
```

Decision tree:

1. Is the VIP in `Service.status.loadBalancer.ingress`?
   - No: controller/status problem.
   - Yes: continue.
2. Is `VIPBinding.status.allocatedIPs` correct and `Ready=True`?
   - No: allocation/conflict/pool problem.
   - Yes: continue.
3. For `mode: l2`, is the speaker advertising?
   - Check logs for `advertising VIPs`.
   - Check the Lease holder.
4. Does ARP resolve?
   - `arping -c 3 <VIP>`.
   - No: speaker/ARP/permissions/interface.
   - Yes: continue.
5. Does TCP work?
   - `nc -vz <VIP> 443`, `curl`.
   - No: kube-proxy/status/return path.
   - Yes: fine.

## VIP is not in EXTERNAL-IP

- Check controller logs:

  ```bash
  kubectl -n easylb-system logs deploy/easylb-controller --tail=200
  ```

- Check `VIPBinding` conditions (`Ready`, `reason`).
- Check `publishNodeIPs` and `nodeIPTypes` if node IPs are missing.
- If the Service has a non-easyLB `loadBalancerClass`, the controller ignores
  it. Check:

  ```bash
  kubectl -n ingress-nginx get svc ... -o jsonpath='{.spec.loadBalancerClass}{"\n"}'
  ```

- Ensure k3s/RKE2 ServiceLB is disabled (`--disable servicelb`).

## VIPBinding is not Ready

| Reason | Action |
|---|---|
| `PoolNotFound` | create the `VIPPool` or fix `poolRef` |
| `PoolExhausted` | add addresses or free a binding |
| `Conflict` | another binding claims the same VIP |
| `InvalidSpec` | use either `vips` or `poolRef`, not both |
| `InvalidVIP` | fix the IP value |
| `RequestedIPUnavailable` | choose another IP or free it |
| `PoolAutoAssignDisabled` | enable `autoAssign` or set `requestedIP` |

## Speaker pod is not ready / crashloops

```bash
kubectl -n easylb-system describe pod -l app.kubernetes.io/component=speaker
kubectl -n easylb-system logs ds/easylb-speaker --tail=200
```

Common causes:

- `operation not permitted` / raw socket error: the speaker is not running as
  root with `NET_RAW`. Check:

  ```bash
  kubectl -n easylb-system get ds easylb-speaker \
    -o jsonpath='{.spec.template.spec.securityContext}{"\n"}'
  ```

  Expect `runAsUser: 0`. If Pod Security Admission is enforced, allow the
  namespace (`pod-security.kubernetes.io/enforce=privileged`).

- `no interface owns address`: the node `InternalIP` is not on any interface or
  `nodeIPType` is wrong. Set `speaker.interface` explicitly or fix the node IP.
- Lease/leader election errors: check RBAC for Leases and the
  `easylb-system` namespace.
- Port conflicts on `8082`/`8083` in hostNetwork: check the node.

## `VIPAlreadyAssigned`

The VIP is already assigned to the node interface, usually by keepalived. For
`mode: l2` this is a conflict: one VIP must have one owner. Remove the VIP from
keepalived or change the binding to `mode: external`.

## ARP does not resolve

```bash
arping -c 3 <VIP>
ip neigh show <VIP>
```

On the node that holds the Lease:

```bash
sudo tcpdump -ni <iface> arp
ip -br a | grep <VIP>
```

- No ARP request at all: client routing/VLAN.
- Request arrives, no reply: speaker not leader, ARP responder failed, or wrong
  interface.
- Reply arrives but the client uses a stale MAC: flush the client ARP cache
  (`ip neigh flush <VIP>`).

## ARP works, TCP fails

- Check kube-proxy rules:

  ```bash
  iptables-save | grep <VIP>
  # or
  ipvsadm -Ln | grep <VIP>
  ```

- If there is no rule, the VIP is not in the Service status or kube-proxy has
  not synced.
- Check the backend Service endpoints.
- Check `externalTrafficPolicy: Local` and a local endpoint on the VIP owner.
- Check return routing when the client is not on the same L2.

## `ping` to an l2 VIP does not work

Expected. The VIP is not assigned to the interface and the speaker only answers
ARP. Use `arping` and TCP checks.

## Stale Lease holder

The Lease holder points to a pod that no longer exists. With the current
speaker, a graceful shutdown releases the Lease immediately; after a hard crash
it expires after `leaseDuration` (15s). If it is stale longer, delete it:

```bash
kubectl -n easylb-system delete lease <lease-name>
```

A current speaker recreates it.

## CRDs / Helm

- `no matches for kind "VIPBinding"`: install `deploy/crds.yaml` or the Helm
  chart first.
- `helm upgrade` does not update CRDs from `crds/`; apply them separately.
- Ownership conflict with old static manifests: delete `deploy/easylb.yaml` and
  `deploy/easylb-speaker.yaml` resources before using the chart.

## Gateway API

- `gatewayAPI.enabled=true` without Gateway API v1 CRDs: the controller fails
  to start. Install the CRDs or disable the feature.
- Installing CRDs fails with `no matches for kind "ValidatingAdmissionPolicy"`
  or `undeclared reference to 'isIP'`: the Gateway API release is too new for
  the cluster. v1.5+ requires Kubernetes 1.31+. Delete the partially installed
  `gateway.networking.k8s.io` CRDs, then install v1.4.1, or upgrade Kubernetes.
- No VIPBinding created: check the `easylb.io/service` annotation, the
  `Gateway.spec.addresses`, and whether the controller actually runs with
  `--enable-gateway-api=true`.
- A VIPBinding exists but the Service has no VIP: the data-plane Service has no
  `loadBalancerClass` while `controller.requireClass` is `true`. Set
  `requireClass: false` or set the class on the Service.
- `BindingConflict` on the Gateway: a `VIPBinding gateway-<name>` already exists
  and is owned by another Gateway.
- `Gateway` is not `Programmed`: check its `GatewayClass`, then the
  implementation logs (for example
  `kubectl -n envoy-gateway-system logs deploy/envoy-gateway`).
- With `externalTrafficPolicy: Local`, no speaker leader is elected on nodes
  without a local gateway endpoint. Check the EndpointSlices and the data-plane
  replica count.
- The data-plane Service name changed after a Gateway upgrade: update the
  annotation.
- The Gateway implementation also owns `spec.addresses`: choose one owner.

## Useful commands

```bash
kubectl -n easylb-system logs deploy/easylb-controller --tail=200
kubectl -n easylb-system logs ds/easylb-speaker --tail=200
kubectl -n easylb-system get lease -o yaml
kubectl get vipbinding -A -o yaml
kubectl -n ingress-nginx get endpoints <service>
kubectl get events -A --sort-by=.lastTimestamp | tail -30
```
