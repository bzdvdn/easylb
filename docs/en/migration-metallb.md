# Migrating from MetalLB

easyLB covers MetalLB's **L2 mode** and the common IPAM/annotation surface used
by small bare-metal clusters. It does not implement BGP, BFD, ECMP, or MetalLB's
advanced traffic engineering; keep MetalLB if you need those features.

## Compatibility matrix

| MetalLB | easyLB | Notes |
|---|---|---|
| `IPAddressPool` | `VIPPool` | `addresses`, `autoAssign`, `avoidBuggyIPs` |
| `IPAddressPool.spec.serviceAllocation` | `VIPPool.spec.serviceAllocation` | namespace/service label selectors |
| `L2Advertisement` | `VIPPool.spec.mode: l2` | no separate advertisement object yet |
| `metallb.io/address-pool` | supported | also `metallb.universe.tf/address-pool` |
| `metallb.io/loadBalancerIPs` | supported | also legacy prefix and `spec.loadBalancerIP` |
| `metallb.io/allow-shared-ip` | supported | `VIPBinding.spec.sharingKey`, non-overlapping ports |
| `metallb.io/ip-allocated-from-pool` | not written | use `VIPBinding.status.allocatedIPs` |
| BGP/BFD/ECMP | not supported | out of scope for easyLB |
| Per-pool node/interface advertisement | not implemented yet | speaker uses global `speaker.interface`/`nodeIPType` |

## Migration steps

1. **Choose an ownership window.** One VIP must have exactly one owner. Do not
   run MetalLB and easyLB speakers for the same address at the same time.
2. **Disable k3s/RKE2 ServiceLB** if you are also replacing klipper:

   ```yaml
   # /etc/rancher/k3s/config.yaml
   disable:
     - servicelb
   ```

   ```bash
   sudo systemctl restart k3s
   ```

3. **Install the Gateway API-independent part of easyLB** (CRDs and chart):

   ```bash
   helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
     --version <chart-version> \
     --namespace easylb-system \
     --create-namespace \
     -f values-easylb.yaml
   ```

4. **Convert each `IPAddressPool` to `VIPPool`:**

   ```yaml
   # MetalLB
   apiVersion: metallb.io/v1beta1
   kind: IPAddressPool
   metadata:
     name: default
     namespace: metallb-system
   spec:
     addresses:
       - 192.168.56.240-192.168.56.249
     autoAssign: true
     avoidBuggyIPs: true
   ---
   # easyLB
   apiVersion: easylb.io/v1alpha1
   kind: VIPPool
   metadata:
     name: default
   spec:
     addresses:
       - 192.168.56.240-192.168.56.249
     autoAssign: true
     avoidBuggyIPs: true
     mode: l2
     serviceAllocation:
       namespaceSelector:
         matchLabels:
           kubernetes.io/metadata.name: production
   ```

5. **Convert `L2Advertisement` intent.** easyLB advertises `mode: l2` pools
   from speaker nodes that are eligible for the Service. There is no separate
   `L2Advertisement` object yet; per-pool node/interface selection is not
   implemented, so all speaker nodes use the global `speaker.interface` and
   `speaker.nodeIPType` settings.

6. **Stop MetalLB before easyLB takes over the addresses:**

   ```bash
   helm uninstall metallb -n metallb-system
   ```

   Keep MetalLB's CRDs until you have removed its custom resources, or delete
   them explicitly. Do not leave both controllers running.

7. **Verify:**

   ```bash
   kubectl get vippool
   kubectl get vipbinding -A -o wide
   kubectl get svc -A --field-selector spec.type=LoadBalancer
   kubectl -n easylb-system get lease
   kubectl -n easylb-system logs ds/easylb-speaker --tail=100
   ```

   A Service without a manual binding should get an `auto-<service>` binding
   and an `EXTERNAL-IP` from a matching pool. `spec.loadBalancerIP`,
   `metallb.io/loadBalancerIPs`, `metallb.io/address-pool`, and
   `metallb.io/allow-shared-ip` continue to work.

8. **Check traffic** with TCP, not ICMP:

   ```bash
   arping -c 3 <VIP>          # IPv4 l2
   ip -6 neigh show <VIP>     # IPv6 l2
   nc -vz <VIP> 443
   curl -k --resolve example.com:443:<VIP> https://example.com/
   ```

## Rollback

1. Uninstall easyLB and delete its `VIPPool`/`VIPBinding` objects.
2. Reinstall MetalLB and recreate the `IPAddressPool`/`L2Advertisement`
   resources.
3. Recreate the Services or let MetalLB reallocate addresses.
4. Clear stale addresses from `Service.status.loadBalancer.ingress` if a
   Service is reused.

## Known gaps

- no BGP/BFD/ECMP;
- no per-pool `L2Advertisement` node/interface selectors;
- no `loadBalancerSourceRanges`;
- Gateway API integration is optional and independent of the MetalLB migration;
- IPv6 VIPs are advertised with NDP, but run an e2e check on your interface and
  kernel before production use.
