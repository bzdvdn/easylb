# Gateway API

**English** | [Русский](../ru/gateway-api.md)

easyLB is a **Service** LoadBalancer controller, not a Gateway API
implementation. It does not implement L7 routing and does not watch
`GatewayClass`, `HTTPRoute`, `GRPCRoute`, or `TLSRoute`. It works with Gateway
API implementations that expose their data plane through a Kubernetes
`Service` of type `LoadBalancer` — which is what most implementations do.

With the optional native integration (`gatewayAPI.enabled: true`), easyLB also
watches `Gateway` objects in the stable `gateway.networking.k8s.io/v1` API and
turns them into `VIPBinding` resources. It reads only the IP values and the
`easylb.io/*` annotations; it never writes `Gateway.status`.

## How it fits

```text
Gateway / HTTPRoute
        │
        ▼
Gateway API implementation (Envoy Gateway, NGINX Gateway Fabric,
Cilium, Traefik, Kong, Istio, ...)
        │ creates / manages
        ▼
data-plane Service (type: LoadBalancer)
        │
        ├── easyLB publishes NodeIP/VIP into status.loadBalancer.ingress
        └── easyLB speaker advertises VIPs with mode: l2
        │
        ▼
kube-proxy -> gateway pods -> HTTPRoute/GRPCRoute backends
```

easyLB only handles L4 addresses for the data-plane Service. L7 routing remains
the responsibility of the Gateway implementation.

## Supported versions

- easyLB watches `Gateway` through `gateway.networking.k8s.io/v1` only. Any
  Gateway API bundle that serves the `v1` `Gateway` (v1.0.0 and newer) works.
  Clusters with only `v1beta1`/`v1alpha2` Gateway CRDs are not supported by the
  native integration; create a `VIPBinding` manually instead.
- The controller uses `unstructured` objects and reads only
  `spec.addresses[].value` for entries with `type: IPAddress`, plus the
  `easylb.io/*` annotations. New fields, new address types, and new resources
  are ignored instead of breaking reconciliation.
- A newer Gateway API release may require a newer Kubernetes. Gateway API
  **v1.5+** uses `ValidatingAdmissionPolicy` and CEL `isIP` and needs
  Kubernetes **1.31+**. On older clusters use **v1.4.1** (or v1.0.0, which
  officially supports Kubernetes 1.25) as long as it serves the `v1` API.
- New Gateway API resources such as `ListenerSet`, `BackendTLSPolicy`,
  `TLSRoute`, or `XBackend` are not interpreted by easyLB. They may still work
  inside the Gateway implementation; easyLB only manages the data-plane
  `Service`.
- Supported implementations are all implementations that create a
  `LoadBalancer` Service for their data plane (Envoy Gateway, NGINX Gateway
  Fabric, Cilium, Traefik, Kong, Istio, ...). There is no
  implementation-specific code path.

The controller also needs `controller.requireClass: false` when the data-plane
Service has no `loadBalancerClass` (the usual case for Gateway
implementations). With `requireClass: false`, easyLB manages every
`LoadBalancer` Service without a class, so use it in clusters where easyLB is
the only such controller, or set the class on the data-plane Service.

## Testing without a cluster

The Gateway-to-`VIPBinding` logic is covered by synthetic unit tests that do
not need Gateway API CRDs or a cluster:

```bash
go test ./internal/controller -run Gateway -v
```

The tests build `Gateway` objects shaped like Gateway API v1.0, v1.2, v1.4,
v1.5, and v1.7 releases, add unknown future fields and resources, and verify
that easyLB still creates the expected binding, applies the address/pool
precedence, ignores `status.addresses`, never overwrites a foreign binding,
and rejects invalid modes. This validates the controller logic, not the real
CRD schema, admission, or conversion — run an end-to-end check on a real
cluster after a Kubernetes/Gateway API upgrade.

## Integration steps

1. Install a Gateway API implementation and create a `Gateway`.
2. Find the data-plane `Service` of type `LoadBalancer` that the implementation
   created. The name is implementation-specific.
3. Create a `VIPBinding` that references that Service, exactly as you would for
   ingress-nginx.
4. Keep `externalTrafficPolicy: Local` on the data-plane Service if you need the
   client source IP.
5. Ensure the Gateway data plane has a local pod on every node that may own the
   VIP, or use `Cluster` policy and accept SNAT.

Minimal `VIPBinding`:

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: gateway-keepalived
  namespace: gateway-system
spec:
  serviceRef:
    name: <data-plane-service>
  mode: external
  vips:
    - 192.168.56.250
```

Or with a pool and the L2 speaker:

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: gateway-easylb
  namespace: gateway-system
spec:
  serviceRef:
    name: <data-plane-service>
  mode: l2
  poolRef:
    name: gateway-vips
```

The same `bindings` map can be used from the Helm chart:

```yaml
bindings:
  gateway-easylb:
    namespace: gateway-system
    serviceRef: <data-plane-service>
    mode: l2
    poolRef: gateway-vips
```

## Native integration (optional)

Instead of creating a `VIPBinding` by hand, you can let easyLB translate a
`Gateway` into a `VIPBinding`.

Enable it:

1. Install the Gateway API **v1** CRDs (`gateway.networking.k8s.io/v1`).
2. Set `gatewayAPI.enabled: true` in the Helm chart (or pass
   `--enable-gateway-api` to the controller).
3. Add annotations to the `Gateway`:

| Annotation | Required | Meaning |
|---|---|---|
| `easylb.io/service` | yes | name of the data-plane `Service` |
| `easylb.io/vip-mode` | no | `external` (default) or `l2` |
| `easylb.io/vip-pool` | no | `VIPPool` name, used when no IP addresses are set |
| `easylb.io/vips` | no | comma-separated fallback VIP list |

4. Put IP addresses into `Gateway.spec.addresses` with `type: IPAddress`. If
   there are no addresses, `easylb.io/vip-pool` or `easylb.io/vips` is used.

Example with an explicit address:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: public
  namespace: gateway-system
  annotations:
    easylb.io/service: envoy-public-abc123
    easylb.io/vip-mode: l2
spec:
  gatewayClassName: envoy-gateway
  addresses:
    - type: IPAddress
      value: 192.168.56.240
  listeners:
    - name: https
      protocol: HTTPS
      port: 443
```

Example using a pool:

```yaml
metadata:
  annotations:
    easylb.io/service: envoy-public-abc123
    easylb.io/vip-mode: l2
    easylb.io/vip-pool: gateway-vips
```

The controller creates a `VIPBinding` named `gateway-<gateway-name>` in the
Gateway namespace, with an owner reference to the Gateway. It is updated when
the Gateway changes and garbage-collected when the Gateway is deleted. The
existing `VIPBinding` controller allocates/status the address; the Service
controller publishes it; the speaker advertises it for `mode: l2`.

Notes:

- If `easylb.io/service` is absent, no binding is created.
- `Gateway.status.addresses` is never written by easyLB.
- If the Gateway implementation already owns `spec.addresses`, do not enable
  the easyLB integration for that Gateway — one VIP, one owner.
- Enabling `gatewayAPI.enabled` without the Gateway API v1 CRDs makes the
  controller fail to start, because the Gateway watch cannot sync.
- Automatic allocation (`auto-<service>`) is skipped when a Gateway-created
  binding exists, so one Service does not get two VIPs.

## Finding the data-plane Service

There is no universal label for this across implementations. In practice:

- Envoy Gateway usually creates a Service named like
  `envoy-<gateway-name>-<hash>`.
- NGINX Gateway Fabric uses `<gateway-name>-nginx`.
- Cilium Gateway uses `cilium-gateway-<gateway-name>`.
- Kong, Traefik, and Istio create their own implementation-specific Service
  names.

Check the Gateway namespace:

```bash
kubectl get svc -n <gateway-namespace> -o wide
kubectl get svc -n <gateway-namespace> -o jsonpath='{range .items[?(@.spec.type=="LoadBalancer")]}{.metadata.name}{"\n"}{end}'
```

Then verify the selector points at the gateway data-plane pods:

```bash
kubectl -n <gateway-namespace> get svc <data-plane-service> -o yaml
kubectl -n <gateway-namespace> get endpoints <data-plane-service>
```

If the implementation lets you set Service annotations or a fixed Service name
(for example, through an `EnvoyProxy` or `NginxProxy` resource), use that to keep
the name stable across upgrades.

## Gateway.spec.addresses vs VIPBinding

Pick one owner per VIP:

- If the implementation honors `Gateway.spec.addresses` and publishes the
  address itself, you may not need easyLB for that Gateway.
- If the implementation expects an external Service LB controller, use a
  `VIPBinding` and do **not** set `Gateway.spec.addresses` at the same time.
- Do not configure both and expect them to reconcile; they will fight over the
  address or produce inconsistent status.

With the native integration enabled, easyLB reads IP values from
`Gateway.spec.addresses`, but it never writes `Gateway.spec.addresses` or
`Gateway.status.addresses`.

## Source IP

The data-plane Service must use `externalTrafficPolicy: Local` for source IP
preservation. This only works if the Gateway data plane has a Ready endpoint on
the node that receives the traffic.

- A DaemonSet-style data plane (one gateway pod per node) is the simplest.
- A Deployment with a few replicas may leave nodes without a local endpoint; with
  `Local`, traffic to the VIP on those nodes is dropped.
- For `mode: l2`, the easyLB speaker only elects a leader on a node with a local
  endpoint (when `Local`), so the VIP lands where a gateway pod exists.
- For `mode: external`, keepalived may move the VIP to a node without a gateway
  pod. Use a `vrrp_script` against the Service `healthCheckNodePort` to lower the
  priority of such nodes (see [source-ip.md](source-ip.md)).

## Verification

```bash
kubectl get gateway -A
kubectl -n <gateway-namespace> get svc <data-plane-service> -o wide
kubectl get vipbinding -A
kubectl -n easylb-system logs ds/easylb-speaker --tail=100

curl -k --resolve example.com:443:<VIP> https://example.com/
```

For an L2 VIP, `ping` will not respond; use `arping` and TCP checks:

```bash
arping -c 3 <VIP>
nc -vz <VIP> 443
```

## Limitations

- easyLB does not implement Gateway API; it only manages the data-plane
  Service.
- The native integration watches only `gateway.networking.k8s.io/v1`. New
  Gateway API resources and fields are ignored, not implemented.
- No `loadBalancerSourceRanges` enforcement.
- No BGP.
- Data-plane Service names may change on Gateway implementation upgrades;
  re-check the `VIPBinding` after upgrades.
- One VIP must have exactly one owner. Do not run keepalived, easyLB `mode: l2`,
  MetalLB, or a Gateway implementation's own LB for the same VIP.
