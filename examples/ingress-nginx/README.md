# ingress-nginx with an easyLB L2 VIP

The easyLB controller auto-creates `auto-<service>` for the ingress-nginx
LoadBalancer Service. No VIPPool binding is written by hand.

## Install ingress-nginx

```bash
helm repo add ingress-nginx https://kubernetes.github.io/ingress-nginx
helm upgrade --install ingress-nginx ingress-nginx/ingress-nginx \
  --namespace ingress-nginx \
  --create-namespace \
  --set controller.service.type=LoadBalancer
```

## Install easyLB

```bash
helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f values.yaml
```

## Verify

```bash
kubectl -n ingress-nginx get svc ingress-nginx-controller -o wide
kubectl -n ingress-nginx get vipbinding
arping -c 3 <VIP>
curl -k --resolve echo.local:443:<VIP> https://echo.local/
```

`publishNodeIPs: false` assumes DNS points only at the VIP. Set it to `true` if
DNS also uses node IPs.

If the VIP is owned by keepalived instead, disable the speaker and use an
explicit binding:

```yaml
speaker:
  enabled: false
bindings:
  ingress-external:
    namespace: ingress-nginx
    serviceRef: ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
```
