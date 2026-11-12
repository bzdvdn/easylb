# Comparison

**English** | [Русский](../ru/comparison.md)

| | klipper / ServiceLB | easyLB | MetalLB | kube-vip | HAProxy + PROXY |
|---|---|---|---|---|---|
| Dataplane | hostPort + iptables in svclb pod | kube-proxy | kube-proxy | kube-proxy | own L4 proxy |
| VIP ownership | none (node IPs) | keepalived (`external`) or easyLB speaker (`l2`) | ARP/NDP or BGP | ARP/NDP, BGP, VRRP | itself |
| IPAM | no | `VIPPool` | `IPAddressPool` | pool/static | no |
| L2 | no | yes (`l2`) | yes | yes | no |
| BGP | no | reserved | yes | yes | no |
| Source IP | lost for VIP traffic that goes through klipper | preserved with `Local` | depends on policy | depends on policy | via PROXY protocol |
| Configuration | implicit in k3s | CRD + Helm | CRD + Helm | CRD/config | config files |
| Privileges | NET_ADMIN + hostPort | controller none; speaker root + NET_RAW | speaker hostNetwork/NET_RAW | hostNetwork/network privileges | hostNetwork |
| Best for | simple node-IP LB | ServiceLB/MetalLB L2 replacement on small bare-metal | full bare-metal LB | VIP/BGP/control-plane VIP | L4 proxy with client IP |

easyLB is intended as a complete replacement for klipper/ServiceLB and for
MetalLB's L2 mode on small bare-metal clusters that do not need BGP or
advanced MetalLB features.

## When easyLB is a good fit

- k3s/RKE2 on bare metal or a private LAN where ServiceLB or MetalLB L2 is
  used today.
- You need `Service` type `LoadBalancer`, stable VIPs, and source IP
  preservation without `svclb` pods.
- You want L2 VIPs with IPAM, ARP advertisement, and failover without BGP.
- You already have VIPs (keepalived/VRRP, hardware LB) and want Kubernetes to
  publish them.
- ingress-nginx or a Gateway API implementation exposes a data-plane Service.
- You want CRD/Helm configuration.

## When it is not

- public cloud/NAT where the destination after NAT is not the address in
  `status` (klipper's hostPort path exists for this).
- `loadBalancerSourceRanges` must be enforced.
- BGP/ECMP or DSR is required.
- You need MetalLB features beyond L2 (BGP, custom advertisements, advanced
  traffic policies).
- You need L7/TLS termination in the LB.

## Coexistence rules

- One VIP has exactly one owner.
- Do not run keepalived, easyLB `mode: l2`, MetalLB, and kube-vip for the same
  VIP.
- `mode: external` + keepalived is a supported combination: keepalived owns the
  VIP, easyLB publishes it.
- Different VIPs can be owned by different systems, but keep the ownership
  explicit and documented.
- Disable the k3s/RKE2 built-in ServiceLB when using easyLB.
