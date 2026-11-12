# Envoy Gateway with an easyLB L2 VIP

This example uses the native Gateway API integration: easyLB watches the
`Gateway` and creates a `VIPBinding` for the Envoy data-plane `Service`.

## Prerequisites

- Gateway API v1 CRDs; see the
  [Gateway API version notes](../../docs/en/gateway-api.md#supported-versions).
- Envoy Gateway installed and its `GatewayClass eg` accepted.

```bash
helm install eg oci://docker.io/envoyproxy/gateway-helm \
  --version <version> \
  --namespace envoy-gateway-system \
  --create-namespace
```

## Install

```bash
kubectl create namespace gateway-system --dry-run=client -o yaml | kubectl apply -f -
kubectl apply -f backend.yaml
kubectl apply -f gateway.yaml

helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f values.yaml

kubectl -n gateway-system wait --for=condition=Programmed gateway/public --timeout=5m
SVC="$(kubectl -n gateway-system get svc \
  -o jsonpath='{range .items[?(@.spec.type=="LoadBalancer")]}{.metadata.name}{"\n"}{end}')"
kubectl -n gateway-system annotate gateway public \
  easylb.io/service="${SVC}" \
  easylb.io/vip-mode=l2 \
  easylb.io/vip-pool=gateway-vips
```

## Verify

```bash
kubectl -n gateway-system get vipbinding gateway-public -o yaml
kubectl -n gateway-system get svc "${SVC}" -o wide
curl -H 'Host: echo.local' http://<VIP>/
```

`gatewayAPI.enabled: true` requires Gateway API v1 CRDs before the easyLB
install. Set `publishNodeIPs: true` if DNS still uses node IPs.
