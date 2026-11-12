# Migrating from ServiceLB (klipper)

k3s/RKE2 ship a built-in ServiceLB (klipper). It creates `svclb` DaemonSet pods
with `hostPort` and DNAT+MASQUERADE, which adds a hop and can hide the client
source IP. easyLB replaces this control plane without proxying packets:
kube-proxy programs the rules from the addresses that easyLB publishes.

This guide covers both migration modes:

- node IPs only — closest to ServiceLB, no VIPs;
- stable VIPs — easyLB L2 speaker or an external VIP owner.

## 1. Disable ServiceLB

On every k3s/RKE2 server node:

```yaml
# /etc/rancher/k3s/config.yaml
disable:
  - servicelb
```

```bash
sudo systemctl restart k3s
```

After the restart, existing `svclb` pods are removed and `LoadBalancer`
Services temporarily have no external address. Install easyLB in the same
window to minimise the gap.

## 2. Install easyLB

```bash
helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f values-easylb.yaml
```

### Option A: node IPs only

This is the smallest step from ServiceLB. easyLB publishes node IPs into
Service status and kube-proxy does the rest.

```yaml
controller:
  publishNodeIPs: true
  requireClass: false

speaker:
  enabled: false

pools: {}
bindings: {}
```

DNS continues to point at node IPs. There is no VIP and no speaker.

### Option B: stable L2 VIPs

```yaml
controller:
  publishNodeIPs: false   # true if DNS still uses node IPs
  requireClass: false

speaker:
  enabled: true

pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true
    mode: l2
    serviceAllocation:
      namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: ingress-nginx
      serviceSelector:
        matchLabels:
          app.kubernetes.io/name: ingress-nginx
```

Update DNS to the allocated VIP after the Service gets an `EXTERNAL-IP`.

### Option C: keep your existing VIP

If keepalived or a hardware load balancer already owns the VIP, do not enable
the L2 speaker. Create an explicit binding:

```yaml
speaker:
  enabled: false

bindings:
  ingress-keepalived:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
```

## 3. Clean up leftover `svclb` objects

```bash
kubectl -n kube-system get ds | grep svclb || true
kubectl -n kube-system get pods | grep svclb || true
```

If any remain after disabling ServiceLB, delete the corresponding
DaemonSets/pods. They are no longer managed once the k3s ServiceLB controller
is disabled.

## 4. Verify

```bash
kubectl -n easylb-system get deploy,ds,pods
kubectl get vippool
kubectl get vipbinding -A
kubectl get svc -A --field-selector spec.type=LoadBalancer
kubectl -n easylb-system get lease
kubectl -n easylb-system logs ds/easylb-speaker --tail=100
```

Expected:

- `EXTERNAL-IP` is populated again (node IPs or VIPs);
- no `svclb` pods;
- for `mode: l2`, `arping`/`ip -6 neigh` and TCP checks work; ICMP echo does not;
- in ingress logs the client IP is preserved with
  `externalTrafficPolicy: Local` and a local ingress endpoint.

## 5. Rollback

1. Uninstall easyLB (and delete its `VIPPool`/`VIPBinding` objects).
2. Remove `disable: [servicelb]` from the k3s config and restart k3s.
3. ServiceLB recreates `svclb` pods for existing `LoadBalancer` Services.

## Notes

- One VIP must have one owner. Do not run easyLB L2 and keepalived on the same
  VIP.
- Node IP mode does not require a pool, bindings, or speaker.
- If you later move from node IPs to VIPs, update DNS and set
  `publishNodeIPs: false` to publish only VIPs.
- For MetalLB users, see [migration-metallb.md](migration-metallb.md).
