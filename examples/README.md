# Examples

Ready-to-use easyLB chart values for common data planes.

| Example | Data plane | VIP source |
|---|---|---|
| [`ingress-nginx`](ingress-nginx/) | ingress-nginx | easyLB L2 pool |
| [`envoy-gateway`](envoy-gateway/) | Envoy Gateway | easyLB L2 pool + Gateway API |
| [`nginx-gateway-fabric`](nginx-gateway-fabric/) | NGINX Gateway Fabric | easyLB L2 pool + Gateway API |

All examples assume:

- k3s/RKE2 ServiceLB is disabled;
- the easyLB chart is installed from `oci://ghcr.io/bzdvdn/charts/easylb`;
- free addresses in `192.168.56.0/24` are replaced with addresses from your
  network;
- `publishNodeIPs: false` is used because DNS points at VIPs. Set it to `true`
  if DNS still uses node IPs.

For keepalived/hardware VIPs, replace the pool with an explicit
`bindings` entry and `mode: external`; see
[configuration](../docs/en/configuration.md#explicit-vips-without-a-pool).
