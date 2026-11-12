# easyLB Helm chart

A lightweight Kubernetes LoadBalancer controller for on-prem, k3s and RKE2
clusters. easyLB publishes node IPs and VIPs into
`Service.status.loadBalancer.ingress` and leaves the dataplane to kube-proxy.

It is a drop-in replacement for the k3s/RKE2 ServiceLB and for MetalLB's L2
mode in small bare-metal clusters that do not need BGP. VIPs can be external
(keepalived/VRRP, a hardware LB) or owned by easyLB's L2 speaker with ARP,
IPv6 NDP, and per-VIP Leases.

- Chart type: application
- Requires: Kubernetes `>=1.24`, Helm 3
- CRDs: `VIPPool`, `VIPBinding` (installed on the first install)
- Source: <https://github.com/bzdvdn/easylb>
- Docs: <https://bzdvdn.github.io/easylb/>
- MetalLB migration: <https://bzdvdn.github.io/easylb/en/migration-metallb/>
- Examples: <https://github.com/bzdvdn/easylb/tree/main/examples>

## Requirements

- Default images are `ghcr.io/bzdvdn/easylb:<appVersion>`. The release workflow
  publishes images to `ghcr.io/<owner>/<repo>`, so forks and private registries
  must override `controller.image.repository`/`tag` and
  `speaker.image.repository`/`tag`.
- k3s/RKE2 ServiceLB (klipper) must be disabled on all server nodes:

  ```yaml
  # /etc/rancher/k3s/config.yaml
  disable:
    - servicelb
  ```

- The speaker opens raw sockets for ARP/NDP and needs `hostNetwork`, root and
  `NET_RAW`. If Pod Security Admission is enforced, the namespace must allow it
  (for example `pod-security.kubernetes.io/enforce=privileged`).
- For `l2` VIPs, choose addresses that are not assigned to any node interface
  and are not owned by keepalived or another VIP owner. One VIP must have
  exactly one owner.
- For the optional Gateway API integration, install Gateway API `v1` CRDs
  first. Gateway API v1.5+ requires Kubernetes 1.31+.

## Install

From the published OCI chart:

```bash
helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml
```

From a local checkout:

```bash
helm upgrade --install easylb charts/easylb \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml
```

Verify:

```bash
kubectl -n easylb-system get deploy,ds,pods
kubectl get vippool
kubectl get vipbinding -A
kubectl -n easylb-system get lease
```

## Values

### Controller

| Value | Default | Description |
|---|---|---|
| `controller.enabled` | `true` | deploy the controller |
| `controller.replicas` | `2` | controller replicas; keep `leaderElect: true` when >1 |
| `controller.image.repository` | `ghcr.io/bzdvdn/easylb` | controller image |
| `controller.image.tag` | chart `appVersion` | controller image tag |
| `controller.image.pullPolicy` | `IfNotPresent` | image pull policy |
| `controller.imagePullSecrets` | `[]` | image pull secrets |
| `controller.leaderElect` | `true` | controller leader election |
| `controller.leaseDuration` | `15s` | leader election Lease duration |
| `controller.renewDeadline` | `10s` | leader election renew deadline |
| `controller.retryPeriod` | `2s` | leader election retry period |
| `controller.loadBalancerClass` | `easylb.io/easylb` | class handled by easyLB |
| `controller.requireClass` | `false` | only handle Services with `loadBalancerClass` |
| `controller.publishNodeIPs` | `true` | publish node IPs together with VIPs |
| `controller.autoAssignServices` | `true` | create `VIPBinding` automatically for matching Services |
| `controller.nodeIPTypes` | `InternalIP` | `InternalIP`, `ExternalIP`, or both |
| `controller.requeueAfter` | `5m` | periodic resync |
| `controller.priorityClassName` | `system-cluster-critical` | priority class |
| `controller.podAnnotations` | `{}` | extra pod annotations |
| `controller.nodeSelector` | `kubernetes.io/os: linux` | node selector |
| `controller.tolerations` | control-plane/master taints | tolerations |
| `controller.topologySpreadConstraints` | hostname spread | spread constraints |
| `controller.podSecurityContext` | non-root, `RuntimeDefault` | pod security context |
| `controller.securityContext` | drop all caps | container security context |
| `controller.resources` | 20m/32Mi → 200m/128Mi | requests/limits |
| `controller.pdb.enabled` | `true` | create a PodDisruptionBudget |
| `controller.pdb.minAvailable` | `1` | PDB `minAvailable` |

### Speaker

| Value | Default | Description |
|---|---|---|
| `speaker.enabled` | `true` | deploy the speaker DaemonSet |
| `speaker.image.repository` | `ghcr.io/bzdvdn/easylb` | speaker image |
| `speaker.image.tag` | chart `appVersion` | speaker image tag |
| `speaker.image.pullPolicy` | `IfNotPresent` | image pull policy |
| `speaker.imagePullSecrets` | `[]` | image pull secrets |
| `speaker.interface` | `""` | ARP interface; auto-detected from `nodeIPType` |
| `speaker.nodeIPType` | `InternalIP` | node address used to select the interface |
| `speaker.loadBalancerClass` | `easylb.io/easylb` | class handled by easyLB |
| `speaker.requireClass` | `false` | only handle Services with `loadBalancerClass` |
| `speaker.leaseDuration` | `15s` | per-VIP Lease duration |
| `speaker.renewDeadline` | `10s` | per-VIP renew deadline |
| `speaker.retryPeriod` | `2s` | per-VIP retry period |
| `speaker.requeueAfter` | `30s` | periodic resync |
| `speaker.priorityClassName` | `system-node-critical` | priority class |
| `speaker.podAnnotations` | `{}` | extra pod annotations |
| `speaker.nodeSelector` | `kubernetes.io/os: linux` | node selector |
| `speaker.tolerations` | control-plane/master/critical-addons | tolerations |
| `speaker.podSecurityContext` | root, `RuntimeDefault` | pod security context |
| `speaker.securityContext` | root + `NET_RAW`, drop all others | container security context |
| `speaker.resources` | 10m/24Mi → 100m/64Mi | requests/limits |

The speaker must run as root: non-root containers do not get an effective
`NET_RAW` capability with the default runtime. It still drops all other
capabilities and uses `NET_RAW` only for the ARP socket.

### Metrics and ServiceMonitor

| Value | Default | Description |
|---|---|---|
| `metrics.service.enabled` | `true` | create ClusterIP Services for controller/speaker metrics |
| `metrics.serviceMonitor.enabled` | `false` | create a Prometheus Operator `ServiceMonitor` |
| `metrics.serviceMonitor.interval` | `30s` | scrape interval |
| `metrics.serviceMonitor.scrapeTimeout` | `10s` | scrape timeout |
| `metrics.serviceMonitor.labels` | `{}` | extra labels for the ServiceMonitor |

The metrics Services expose controller metrics on `8080` and speaker metrics on
`8082`, both under the port name `metrics`. `ServiceMonitor` requires the
Prometheus Operator CRDs; it is disabled by default so the chart renders
without them.

### Gateway API

| Value | Default | Description |
|---|---|---|
| `gatewayAPI.enabled` | `false` | watch Gateway API `v1` `Gateway` objects and create `VIPBinding` |

`gatewayAPI.enabled: true` requires the Gateway API v1 CRDs. If they are
missing, the controller cannot sync the Gateway watch and does not start.
When the data-plane Service has no `loadBalancerClass`, keep
`controller.requireClass: false` (the chart default) or set the class on that
Service. With `requireClass: false`, easyLB manages every classless
`LoadBalancer` Service in the cluster.

### Pools and bindings

`pools` and `bindings` are maps: the key becomes the object name. Both are
rendered as part of the Helm release.

```yaml
pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true

bindings:
  ingress-keepalived:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
  ingress-easylb:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: l2
    poolRef: ingress-vips
```

Binding fields: `namespace`, `serviceRef`, `mode` (`external` or `l2`), `vips`,
`poolRef`, `requestedIP`, `sharingKey`. Use either `vips` or `poolRef`, not both.
`pools` is optional: an explicit `vips` binding works without any pool and is
never overwritten by automatic allocation.

Pool fields: `addresses`, `autoAssign`, `avoidBuggyIPs`, `mode` (`l2` by
default), and `serviceAllocation.namespaceSelector` /
`serviceAllocation.serviceSelector`.
When `autoAssign` is enabled and a matching `LoadBalancer` Service has no
`VIPBinding`, the controller creates `auto-<service>` with an owner reference to
the Service. Deleting the Service deletes the binding and releases the VIP.
Matching pools are tried in name order and exhausted pools are skipped.
Manual and Gateway-created bindings take precedence.

MetalLB compatibility for auto-allocated Services:

- `spec.loadBalancerIP` and `metallb.io/loadBalancerIPs` (legacy
  `metallb.universe.tf/loadBalancerIPs`) request explicit addresses;
- `metallb.io/address-pool` (legacy `metallb.universe.tf/address-pool`) selects
  a pool by name;
- `metallb.io/allow-shared-ip` (legacy `metallb.universe.tf/allow-shared-ip`)
  shares a VIP between Services with the same key and non-overlapping ports;
- `easylb.io/auto-assign: "false"` on the Service disables auto-allocation.

## Examples

Ingress-nginx with a keepalived VIP and an easyLB L2 VIP:

```yaml
controller:
  image:
    repository: ghcr.io/bzdvdn/easylb
    tag: <tag>
  publishNodeIPs: false

speaker:
  enabled: true
  image:
    repository: ghcr.io/bzdvdn/easylb
    tag: <tag>

pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true

bindings:
  ingress-keepalived:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
  ingress-easylb:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: l2
    poolRef: ingress-vips
```

Gateway API with native integration:

```yaml
controller:
  publishNodeIPs: false
  requireClass: false

speaker:
  enabled: true

gatewayAPI:
  enabled: true

pools:
  gateway-vips:
    addresses:
      - 192.168.56.241-192.168.56.249
    autoAssign: true
```

Then annotate the `Gateway`:

```bash
kubectl -n <gateway-namespace> annotate gateway <gateway-name> \
  easylb.io/service=<data-plane-service> \
  easylb.io/vip-mode=l2 \
  easylb.io/vip-pool=gateway-vips
```

A complete runnable example is in
[`values-example.yaml`](values-example.yaml).

## CRDs and upgrades

- CRDs in `crds/` are installed on the first `helm install`. Helm **does not
  update** CRDs on `helm upgrade`; apply `deploy/crds.yaml` separately when the
  schema changes.
- `helm uninstall` does not remove CRDs. Deleting a CRD deletes all
  `VIPPool`/`VIPBinding` objects.
- `VIPPool`/`VIPBinding` rendered from `pools`/`bindings` are part of the
  release and are removed on uninstall. Objects created manually or by
  automatic allocation (`auto-<service>`) are not.

## Uninstall and cleanup

```bash
helm uninstall easylb -n easylb-system
```

easyLB does not install iptables/ipvs rules; kube-proxy creates them from
`Service.status.loadBalancer.ingress`. Uninstall removes the controller,
speaker, RBAC, PDB, and the chart-rendered `VIPPool`/`VIPBinding` objects. It
does **not** clear published Service status, remove CRDs, remove manually
created bindings, or delete the namespace.

To clean up fully:

```bash
# Remove bindings created outside the release (chart-rendered ones are gone).
kubectl get vipbinding -A
kubectl get vippool
kubectl delete vipbinding <name> -n <namespace>
kubectl delete vippool <name>

# Clear published addresses if the Services are kept.
kubectl -n <namespace> patch svc <service> --type merge \
  -p '{"status":{"loadBalancer":{"ingress":[]}}}' --subresource=status

# Remove stale runtime Leases if the namespace is kept.
kubectl -n easylb-system get lease
kubectl -n easylb-system delete lease <name>

# Optional and destructive: removes all VIPPool/VIPBinding objects.
kubectl delete crd vippools.easylb.io vipbindings.easylb.io
```

With `mode: l2`, the speaker stops answering ARP when its pods terminate. With
`mode: external`, the VIP keeps working while keepalived owns it and the address
stays in Service status. See
[docs/en/operations.md](../../docs/en/operations.md#what-uninstall-cleans-up)
for the full behavior.

## Development

```bash
helm lint charts/easylb
helm template easylb charts/easylb --namespace easylb-system \
  -f charts/easylb/values-example.yaml --include-crds
```
