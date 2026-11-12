# 0003 — L2 speaker на per-VIP Lease

**Статус:** Accepted

## Проблема

В некоторых кластерах нет keepalived, но нужны VIP, анонсируемые в LAN. VIP
должен переезжать между нодами при отказе владельца.

## Ограничения

- Нет внешней БД.
- Kubernetes API уже является зависимостью.
- Failover не должен оставлять stale VIP на старой ноде.
- Малые и средние кластеры, не тысячи VIP.

## Варианты

1. Memberlist/leader election как в MetalLB.
2. Kubernetes `coordination.k8s.io/Lease` на каждый VIP.
3. Полагаться на keepalived/VRRP для всех VIP.

## Решение

Вариант 2: отдельный Lease на `VIPBinding` + userspace ARP/NDP responders. VIP не
назначается на интерфейс.

## Почему

- Переиспользует API-сервер и существующий RBAC.
- Не нужно эксплуатировать новый membership-протокол.
- Holder виден через `kubectl get lease`.
- `ReleaseOnCancel` даёт быстрый graceful failover; crash — по истечении
  `leaseDuration`.
- VIP не назначается, поэтому упавшая нода не оставляет stale-адрес.

## Последствия

- Failover зависит от доступности API.
- ARP responder требует `AF_PACKET`/`NET_RAW`.
- Один VIP — один Lease; много VIP — много маленьких Lease.

## Когда пересмотреть

Требования к работе при недоступном API, очень большое число VIP или нужен
memberlist-подобный детект отказов.
