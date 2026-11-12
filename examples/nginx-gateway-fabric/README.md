# NGINX Gateway Fabric with an easyLB L2 VIP

This example uses the native Gateway API integration with NGINX Gateway
Fabric. The NGINX data-plane Service is named `<gateway>-nginx`.

## Prerequisites

- Gateway API v1.4.1 CRDs (NGF 2.4.x compatibility; see the
  [Gateway API version notes](../../docs/en/gateway-api.md#supported-versions)).
- NGINX Gateway Fabric installed:

```bash
kubectl apply --server-side -f \
  https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.4.1/standard-install.yaml

helm install ngf oci://ghcr.io/nginx/charts/nginx-gateway-fabric \
  --version 2.4.2 \
  --namespace nginx-gateway \
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
kubectl -n gateway-system annotate gateway public \
  easylb.io/service=public-nginx \
  easylb.io/vip-mode=l2 \
  easylb.io/vip-pool=gateway-vips
```

If the Gateway name or namespace differs, update the `easylb.io/service`
annotation to `<gateway-name>-nginx`.

## Verify

```bash
kubectl -n gateway-system get vipbinding gateway-public -o yaml
kubectl -n gateway-system get svc public-nginx -o wide
curl -H 'Host: echo.local' http://<VIP>/
```
