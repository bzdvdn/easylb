# Архитектура

[English](../en/architecture.md) | **Русский**

easyLB — control-plane контроллер и опциональный per-node L2-speaker. Пакеты
он не проксирует: DNAT и балансировку выполняет kube-proxy, как для любого
Kubernetes Service.

## Компоненты

```text
                    Kubernetes API
                         │
        ┌────────────────┴────────────────┐
        │                                 │
  easyLB controller                 easyLB speaker
  (Deployment, 2 реплики)           (DaemonSet, hostNetwork)
        │                                 │
        │ VIPPool / VIPBinding             │ Lease на VIPBinding
        │ status.loadBalancer.ingress      │ ARP/NDP + gratuitous ARP
        ▼                                 ▼
   kube-proxy на всех нодах        VIP (mode: l2)
        │
        ▼
   ingress-nginx / pods
```

### Controller

Deployment в `easylb-system`, 2 реплики, leader election через Lease
`easylb.io`. Активен только лидер.

Смотрит:

- `Service` (type LoadBalancer);
- `Node` (адреса и readiness);
- `EndpointSlice` (локальные Ready endpoints для `externalTrafficPolicy:
  Local`);
- `VIPBinding` и `VIPPool`.

Делает:

- выделяет VIP из пула (если `poolRef`) и пишет в
  `VIPBinding.status.allocatedIPs`;
- собирает адреса для Service: IP нод (если `publishNodeIPs`) + VIP из
  binding'ов;
- патчит `status.loadBalancer.ingress` с optimistic lock;
- обновляет `conditions` у `VIPBinding`;
- при включённой автоаллокации создаёт `auto-<service>` bindings для подходящих
  Service и чистит их через ownerReference;
- не меняет `spec` Service и не трогает внешних владельцев VIP.

Политика выбора IP нод:

- `externalTrafficPolicy: Local` — только Ready-ноды, на которых есть Ready
  endpoint;
- `externalTrafficPolicy: Cluster` — все Ready-ноды;
- тип адреса задаётся `--node-ip-types` (по умолчанию `InternalIP`).

### Speaker

Опционален (`speaker.enabled`). DaemonSet на каждой ноде, `hostNetwork`, root
+ `NET_RAW`, потому что открывает raw-сокеты для ARP и IPv6 NDP.

Смотрит `VIPBinding`, `Node`, `Service`, `EndpointSlice`. Обрабатывает только
`mode: l2`; `mode: external` полностью игнорирует.

Для каждого `VIPBinding`:

1. Проверяет eligibility:
   - нода Ready;
   - есть хотя бы один Ready endpoint;
   - при `Local` — Ready endpoint именно на этой ноде.
2. Выбирает интерфейс:
   - `--interface`, если задан;
   - иначе интерфейс, на котором висит `InternalIP`/`ExternalIP` ноды.
3. Проверяет, что VIP ещё не назначен на интерфейс. Если назначен — не
   анонсирует и пишет событие `VIPAlreadyAssigned` (защита от конфликта с
   keepalived).
4. Участвует в leader election через `coordination.k8s.io/Lease`
   (отдельный Lease на `VIPBinding`).
5. Лидер запускает ARP responder для IPv4 и NDP responder для IPv6: они
   отвечают на ARP/neighbor solicitation для VIP и шлют gratuitous
   ARP/unsolicited neighbor advertisement при takeover.
6. При потере Lease/eligibility/удалении binding останавливает responder.

VIP **не назначается на интерфейс** и не добавляется в `ip addr`. Ядро не
отвечает ни за ARP/NDP, ни за ICMP для такого адреса: ARP/NDP делает speaker,
а ICMP echo не отвечает вообще. TCP/UDP обрабатывает kube-proxy по правилам
`KUBE-EXT`.

### Gateway API

Реализации Gateway API поднимают data-plane `Service` типа `LoadBalancer`.
easyLB управляет этим Service так же, как ingress-nginx. При опциональной
интеграции `--enable-gateway-api` `Gateway` превращается в `VIPBinding`; см.
[gateway-api.md](gateway-api.md).

### Dataplane

kube-proxy на каждой ноде:

- читает `status.loadBalancer.ingress`;
- ставит `KUBE-EXT` правила для NodeIP/VIP;
- делает DNAT на endpoint;
- ведёт conntrack.

easyLB не добавляет ни одного правила в netfilter и не находится в пути
пакета. Поэтому:

- рестарт controller/speaker не рвёт established-соединения;
- долгие TCP/stream-сессии живут по таймаутам conntrack/kube-proxy;
- при падении ноды-владельца VIP или backend-поды established-сессии
  рвутся — это свойство L4 VIP, а не easyLB.

### Keepalived

В режиме `external` keepalived остаётся владельцем VIP:

- назначает VIP на интерфейс;
- делает VRRP/ARP;
- двигает VIP между нодами.

easyLB только публикует VIP в статусе сервиса. На ноде-владельце VIP должен
быть локальный ingress endpoint; иначе при `Local` трафик будет дропаться.
Для этого в keepalived полезно добавить `vrrp_script` на
`healthCheckNodePort` сервиса (см. [source-ip.md](source-ip.md)).

## Потоки трафика

### NodeIP

```text
DNS -> NodeIP:443
       -> kube-proxy KUBE-EXT (NodeIP есть в status)
       -> локальный ingress pod (Local)
       -> source IP клиента
```

### External VIP (keepalived)

```text
DNS -> VIP:443
       -> keepalived: VIP назначен на ноду, ARP отвечает ядро
       -> kube-proxy KUBE-EXT
       -> локальный ingress pod
       -> source IP клиента
```

### L2 VIP (easyLB speaker)

```text
DNS -> VIP:443
       -> speaker: ARP отвечает userspace, VIP на интерфейсе нет
       -> kube-proxy KUBE-EXT
       -> локальный ingress pod
       -> source IP клиента

ICMP ping -> без ответа (ожидаемо)
```

## Модель владения VIP

Один VIP — ровно один владелец:

| Режим | Владелец | Где настраивается |
|---|---|---|
| `external` | keepalived/администратор | keepalived + `VIPBinding(mode: external)` |
| `l2` | easyLB speaker | `VIPBinding(mode: l2)` |
| `bgp` | будущий easyLB speaker | зарезервировано |

Нельзя одновременно держать один VIP в keepalived и в `mode: l2`. Speaker
проверяет только локальный интерфейс ноды-лидера, поэтому конфликт между
нодами надо исключать конфигурацией.

## Отказоустойчивость

### Controller

- 2 реплики, leader election (`Lease easylb.io`, `easylb-system`);
- PDB `minAvailable: 1`, spread по `kubernetes.io/hostname`;
- rolling update `maxUnavailable: 0`, `maxSurge: 1`;
- `LeaderElectionReleaseOnCancel: true`;
- optimistic lock при patch status.

Падение лидера: standby берёт Lease, статус не меняется. Существующий трафик
не зависит от контроллера.

### Speaker

- Lease на каждый `VIPBinding`;
- `ReleaseOnCancel: true` и контекст процесса: при SIGTERM Lease освобождается
  сразу;
- при crash Lease истекает через `leaseDuration` (по умолчанию 15s);
- новый лидер шлёт gratuitous ARP.

При failover VP существующие TCP-сессии рвутся, новые идут на нового
владельца.

### Dataplane

kube-proxy и conntrack — штатные компоненты k3s. Их отказоустойчивость —
ответственность кластера, не easyLB.

## Namespaces, RBAC, CRD

- `easylb-system` — controller, speaker, Leases.
- `VIPPool` — cluster-scoped.
- `VIPBinding` — namespaced, обычно в namespace Service.
- Controller RBAC: services/status, nodes, endpointslices, events, leases,
  vipbindings/status, vippools и Gateways (при опциональной Gateway-интеграции).
- Speaker RBAC: services, nodes, endpointslices, vipbindings, leases, events.

## Архитектурные решения

- [0001 — publish-only controller](decisions/0001-publish-only-controller.md)
- [0002 — CRD как единственный источник истины](decisions/0002-crd-as-source-of-truth.md)
- [0003 — L2 speaker на Lease](decisions/0003-l2-speaker-with-leases.md)
- [0004 — speaker root + NET_RAW](decisions/0004-speaker-root-net-raw.md)
- [0005 — l2 VIP не назначается на интерфейс](decisions/0005-l2-vip-not-assigned.md)
- [0006 — HA controller](decisions/0006-controller-ha.md)
