# Сравнение

[English](../en/comparison.md) | **Русский**

| | klipper / ServiceLB | easyLB | MetalLB | kube-vip | HAProxy + PROXY |
|---|---|---|---|---|---|
| Dataplane | hostPort + iptables в svclb-поде | kube-proxy | kube-proxy | kube-proxy | собственный L4-прокси |
| Владение VIP | нет (IP нод) | keepalived (`external`) или easyLB speaker (`l2`) | ARP/NDP или BGP | ARP/NDP, BGP, VRRP | сам |
| IPAM | нет | `VIPPool` | `IPAddressPool` | пул/статика | нет |
| L2 | нет | да (`l2`) | да | да | нет |
| BGP | нет | зарезервировано | да | да | нет |
| Source IP | теряется для VIP, идущего через klipper | сохраняется при `Local` | зависит от политики | зависит от политики | через PROXY protocol |
| Конфигурация | неявно в k3s | CRD + Helm | CRD + Helm | CRD/config | конфиги |
| Привилегии | NET_ADMIN + hostPort | controller без привилегий; speaker root + NET_RAW | speaker hostNetwork/NET_RAW | hostNetwork/сетевые привилегии | hostNetwork |
| Для чего | простой LB на IP нод | замена ServiceLB и MetalLB L2 для небольших bare-metal | полноценный bare-metal LB | VIP/BGP/control-plane VIP | L4-прокси с client IP |

easyLB задуман как полноценная замена klipper/ServiceLB и L2-режима MetalLB
для небольших bare-metal кластеров, которым не нужен BGP и продвинутые
возможности MetalLB.

## Когда easyLB подходит

- k3s/RKE2 на bare metal или в приватной LAN, где сейчас используется
  ServiceLB или MetalLB L2.
- Нужен `Service` типа `LoadBalancer`, стабильные VIP и source IP без
  `svclb`-подов.
- Нужны L2 VIP с IPAM, ARP-анонсом и failover без BGP.
- Уже есть VIP (keepalived/VRRP, аппаратный LB), и хочется публиковать его из
  Kubernetes.
- ingress-nginx или Gateway API поднимает data-plane Service.
- Хочется CRD/Helm-конфигурацию.

## Когда не подходит

- public cloud/NAT, где destination после NAT не совпадает с адресом в
  `status` (для этого в klipper есть hostPort-путь).
- Нужен enforcement `loadBalancerSourceRanges`.
- Нужен BGP/ECMP или DSR.
- Нужны возможности MetalLB сверх L2 (BGP, кастомные анонсы, продвинутые
  traffic policies).
- Нужен L7/TLS termination в LB.

## Правила сосуществования

- Один VIP — ровно один владелец.
- Нельзя одновременно держать один VIP на keepalived, easyLB `mode: l2`,
  MetalLB и kube-vip.
- `mode: external` + keepalived — поддерживаемая комбинация: keepalived владеет
  VIP, easyLB его публикует.
- Разные VIP могут принадлежать разным системам, но владение должно быть
  явным и задокументированным.
- При использовании easyLB отключи штатный ServiceLB k3s/RKE2.
