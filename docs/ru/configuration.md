# Конфигурация

[English](../en/configuration.md) | **Русский**

Единственный источник истины для VIP — CRD `VIPPool` и `VIPBinding`.
Аннотации на Service (`easylb.io/vips`, `easylb.io/vip-config`) удалены и
игнорируются.

## VIPPool

Cluster-scoped пул адресов.

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPPool
metadata:
  name: ingress-vips
spec:
  addresses:
    - 192.168.56.240-192.168.56.249
    - 192.168.57.10/32
  autoAssign: true
```

Поля:

| Поле | Описание |
|---|---|
| `spec.addresses` | список IP, CIDR или диапазонов `a-b`; IPv4 и IPv6 |
| `spec.autoAssign` | разрешить автоматическую аллокацию (по умолчанию `true`) |

Ограничения:

- пул разворачивается максимум в 4096 адресов;
- адреса не проверяются на занятость в сети — только на пересечения между
  `VIPBinding` и аннотациями больше нет;
- один пул может использоваться несколькими binding'ами.

## VIPBinding

Namespaced объект, обычно в namespace Service.

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: ingress-easylb
  namespace: ingress-nginx
spec:
  serviceRef:
    name: my-release-ingress-nginx-controller
  mode: l2
  poolRef:
    name: ingress-vips
```

Поля:

| Поле | Описание |
|---|---|
| `spec.serviceRef.name` | имя `Service` в этом же namespace |
| `spec.mode` | `external` (по умолчанию), `l2`; `bgp` зарезервирован |
| `spec.vips` | явный список VIP; взаимоисключающе с `poolRef` |
| `spec.poolRef.name` | имя `VIPPool` |
| `spec.requestedIP` | конкретный IP из пула; только с `poolRef` |

Правила:

- либо `vips`, либо `poolRef`; оба одновременно — ошибка;
- один и тот же IP не может быть заявлен двумя binding'ами;
- `mode` относится ко всему binding'у. Для смешанного случая (часть VIP на
  keepalived, часть на easyLB) создаются **два binding'а** на один Service.

### Статус

```yaml
status:
  allocatedIPs:
    - 192.168.56.240
  observedGeneration: 1
  conditions:
    - type: Ready
      status: "True"
      reason: Allocated
      message: address allocated from pool
```

Возможные причины (`reason`):

| Reason | Значение |
|---|---|
| `Explicit` | VIP взяты из `spec.vips` |
| `Allocated` | адрес выделен из пула |
| `InvalidSpec` | нарушены правила (vips+poolRef, нет vips/poolRef и т.п.) |
| `InvalidVIP` | некорректный IP |
| `Conflict` | IP уже занят другим binding'ом |
| `PoolNotFound` | пул не найден; предыдущая аллокация сохраняется |
| `PoolExhausted` | в пуле нет свободных адресов |
| `PoolAutoAssignDisabled` | `autoAssign: false` без `requestedIP` |
| `RequestedIPUnavailable` | `requestedIP` вне пула или уже занят |
| `AllocationError` | ошибка API/чтения |

При временной ошибке пула easyLB не очищает предыдущую аллокацию, чтобы не
уронить трафик.

## Примеры

### Keepalived VIP (внешний владелец)

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: ingress-keepalived
  namespace: ingress-nginx
spec:
  serviceRef:
    name: my-release-ingress-nginx-controller
  mode: external
  vips:
    - 192.168.56.250
```

easyLB только публикует `.250` в статусе. VIP на интерфейсе, ARP/VRRP и
failover — keepalived. Speaker этот binding игнорирует.

### easyLB L2 VIP из пула

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: ingress-easylb
  namespace: ingress-nginx
spec:
  serviceRef:
    name: my-release-ingress-nginx-controller
  mode: l2
  poolRef:
    name: ingress-vips
  # requestedIP: 192.168.56.240
```

Controller выделит первый свободный адрес пула и запишет его в
`status.allocatedIPs`. Speaker будет анонсировать его через ARP.

### Два VIP на один Service (смешанный режим)

Создаются два binding'а, как в примере выше. Controller объединит VIP и
опубликует оба в одном `Service.status.loadBalancer.ingress`.

## IP нод

Управляется флагом controller'а (и значением Helm):

```yaml
controller:
  publishNodeIPs: true
  nodeIPTypes: InternalIP
```

- `true` — IP нод добавляются в статус вместе с VIP. Нужно, если DNS
  указывает на IP нод.
- `false` — публикуются только VIP из binding'ов. DNS на IP нод перестанет
  работать для этого Service (если IP не в `spec.externalIPs`).
- `nodeIPTypes` — `InternalIP`, `ExternalIP` или оба через запятую.
- при `Local` публикуются только Ready-ноды с Ready endpoint;
- при `Cluster` — все Ready-ноды.

## Controller: флаги

| Флаг | По умолчанию | Назначение |
|---|---|---|
| `--load-balancer-class` | `easylb.io/easylb` | класс Service |
| `--require-class` | `true` | требовать `spec.loadBalancerClass` |
| `--enable-gateway-api` | `false` | смотреть `Gateway` в `gateway.networking.k8s.io/v1` и создавать `VIPBinding` |
| `--node-ip-types` | `InternalIP` | какие адреса нод публиковать |
| `--publish-node-ips` | `true` | публиковать ли IP нод |
| `--leader-elect` | `false` | leader election |
| `--leader-election-namespace` | `easylb-system` | namespace Lease |
| `--leader-election-lease-duration` | `15s` | Lease duration |
| `--leader-election-renew-deadline` | `10s` | renew deadline |
| `--leader-election-retry-period` | `2s` | retry period |
| `--requeue-after` | `5m` | периодический resync |

## Speaker: флаги

| Флаг | По умолчанию | Назначение |
|---|---|---|
| `--lease-namespace` | `easylb-system` | namespace per-VIP Lease |
| `--node-ip-type` | `InternalIP` | адрес ноды для выбора интерфейса |
| `--interface` | пусто | интерфейс ARP; автоопределение |
| `--lease-duration` | `15s` | Lease duration |
| `--renew-deadline` | `10s` | renew deadline |
| `--retry-period` | `2s` | retry period |
| `--requeue-after` | `30s` | периодический resync |

## Helm values

Структура:

```yaml
controller:
  enabled: true
  replicas: 2
  image:
    repository: <registry>/easylb
    tag: <tag>
  publishNodeIPs: true
  nodeIPTypes: InternalIP
  requireClass: false        # нужно для data-plane Service без класса (Gateway API)
  leaderElect: true
  pdb:
    enabled: true
    minAvailable: 1

speaker:
  enabled: true
  image:
    repository: <registry>/easylb
    tag: <tag>
  interface: ""
  nodeIPType: InternalIP

gatewayAPI:
  enabled: false

pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true

bindings:
  ingress-keepalived:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
  ingress-easylb:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: l2
    poolRef: ingress-vips
```

`pools` и `bindings` — map'ы: ключ становится именем объекта. Полный пример:
`charts/easylb/values-example.yaml`.

Для нативной интеграции с Gateway API включи `gatewayAPI.enabled: true` и
сначала поставь Gateway API `v1` CRD. Data-plane Service реализаций Gateway
обычно без `loadBalancerClass`; либо оставь `controller.requireClass: false`
(тогда easyLB берёт все `LoadBalancer` Service без класса в кластере), либо
явно задай `loadBalancerClass: easylb.io/easylb` на этом Service.

## Helm и CRD

- CRD лежат в `charts/easylb/crds/` и устанавливаются при первом `helm install`.
- `helm upgrade` **не обновляет** CRD из `crds/`. При изменении схемы
  применяйте `deploy/crds.yaml` отдельно.
- `helm uninstall` не удаляет CRD (стандартное поведение Helm).

## Переход с аннотаций

Аннотации удалены. Если на Service остались `easylb.io/vips` или
`easylb.io/vip-config`, они игнорируются. Создайте `VIPBinding` на тот же
Service; после этого controller начнёт публиковать VIP из binding'а.

## Конфликты

- Один VIP не может быть в двух binding'ах.
- Один VIP не может быть одновременно в `mode: l2` и в keepalived.
- `VIP:port` должен быть уникален: два разных Service на одном `VIP:80`
  невозможны; для разных доменов на одном порту используйте один ingress с
  Host/SNI-роутингом.
