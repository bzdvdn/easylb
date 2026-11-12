# Source IP

**English** | [Русский](../ru/source-ip.md)

Client source IP preservation is determined by how the packet reaches the
backend pod: DNAT, SNAT, routing, and `externalTrafficPolicy`. It is not
something easyLB adds or removes.

## Matrix

| Ingress | Policy | What ingress sees | Source IP |
|---|---|---|---|
| NodeIP | `Local` + local endpoint | real client | preserved |
| NodeIP | `Cluster` | node/gateway IP (SNAT possible) | not guaranteed |
| External VIP (keepalived) | `Local` + endpoint on the VIP owner | real client | preserved |
| External VIP (keepalived) | `Cluster` | SNAT possible | not guaranteed |
| L2 VIP (speaker) | `Local` + endpoint on the leader | real client | preserved |
| L2 VIP (speaker) | `Cluster` | SNAT possible | not guaranteed |

## Why keepalived VIPs lost source IP in klipper

k3s ServiceLB runs `svclb` pods with `hostPort`. kube-proxy only knows the
addresses from `status.loadBalancer.ingress` (usually node IPs). A keepalived
VIP was not in that list, so:

```text
client -> VIP -> hostPort -> svclb pod
                              DNAT + MASQUERADE (in the pod netns)
                              -> ingress sees the svclb pod IP
```

easyLB publishes the VIP in the status. kube-proxy gets the address and performs
DNAT itself, without an intermediate pod and without masquerading external
clients under `Local`.

## Requirements for Local

1. `externalTrafficPolicy: Local` on the Service.
2. A Ready endpoint for that Service on the node that received the traffic.
   For ingress-nginx this is usually a DaemonSet on every node.
3. For `l2`, the easyLB speaker only elects a leader on a node with a local
   Ready endpoint (when `Local`).
4. For `external` VIPs, keepalived must keep the VIP on a node with a local
   endpoint. Otherwise `Local` traffic is dropped.

## Keepalived: health check against the local endpoint

A Service with `externalTrafficPolicy: Local` has a `healthCheckNodePort` that
kube-proxy serves on the node and that returns 200 only when a local endpoint
exists. It is a good fit for `vrrp_script`:

```conf
vrrp_script chk_ingress {
    script "/usr/bin/curl -sf http://127.0.0.1:<healthCheckNodePort>/healthz"
    interval 2
    fall 3
    rise 2
    weight -20
}

vrrp_instance VI_INGRESS {
    state BACKUP
    interface enp0s8
    virtual_router_id 51
    priority 150
    advert_int 1
    nopreempt
    unicast_src_ip 192.168.56.142
    unicast_peer {
        192.168.56.143
    }
    virtual_ipaddress {
        192.168.56.250/32 dev enp0s8
    }
    track_script {
        chk_ingress
    }
}
```

If the local ingress is unhealthy, the priority drops and the VIP moves to a
node with a healthy endpoint.

```bash
kubectl -n ingress-nginx get svc my-release-ingress-nginx-controller \
  -o jsonpath='{.spec.healthCheckNodePort}{"\n"}'
```

## L2 VIP and ICMP

An `l2` VIP is not assigned to the interface. The speaker answers ARP for IPv4
and NDP for IPv6, but never ICMP echo, so:

- `ping` does not work — expected;
- `arping` works for IPv4; use `ip -6 neigh` and TCP checks for IPv6;
- TCP/UDP through kube-proxy works;
- use TCP health checks (`curl`, `/healthz`, `nc`), not ICMP.

This is the same model as MetalLB L2.

## DNS and failover

- DNS to node IPs does not know about node failure. Use a VIP for HA.
- On a VIP owner change, established TCP sessions break: the new node does not
  know the old conntrack. Clients must reconnect.
- For keepalived, use VRRP priorities and a health script.
- For `l2`, use Leases and, if needed, shorter timings (`lease-duration`,
  `renew-deadline`, `retry-period`).

## Verification

From a client:

```bash
curl -k -H 'Host: echo1.local' https://192.168.56.250/
curl -k -H 'Host: echo1.local' https://192.168.56.240/
arping -c 3 192.168.56.240
ip neigh show 192.168.56.240
```

Ingress logs:

```bash
kubectl -n ingress-nginx logs ds/my-release-ingress-nginx-controller --tail=100
```

L4 check:

```bash
sudo tcpdump -ni <iface> host 192.168.56.240 and port 443
```

If the ingress log shows the client IP, source IP is preserved. If it shows a
node IP or the `svclb` pod IP, check `externalTrafficPolicy`, the local
endpoint, and the VIP mode.
