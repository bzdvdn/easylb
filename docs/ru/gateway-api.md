# Gateway API

[English](../en/gateway-api.md) | **Русский**

easyLB — это LoadBalancer controller для **Service**, а не реализация Gateway
API. Он не реализует L7-роутинг и не смотрит `GatewayClass`, `HTTPRoute`,
`GRPCRoute` или `TLSRoute`. Он работает с теми реализациями Gateway API,
которые поднимают data-plane через Kubernetes `Service` типа `LoadBalancer` —
а так делают почти все.

С опциональной нативной интеграцией (`gatewayAPI.enabled: true`) easyLB также
смотрит `Gateway` в стабильном API `gateway.networking.k8s.io/v1` и превращает
его в `VIPBinding`. Он читает только IP-значения и аннотации `easylb.io/*`,
`Gateway.status` никогда не пишет.

## Как это устроено

```text
Gateway / HTTPRoute
        │
        ▼
реализация Gateway API (Envoy Gateway, NGINX Gateway Fabric,
Cilium, Traefik, Kong, Istio, ...)
        │ создаёт / управляет
        ▼
data-plane Service (type: LoadBalancer)
        │
        ├── easyLB публикует NodeIP/VIP в status.loadBalancer.ingress
        └── easyLB speaker анонсирует VIP с mode: l2
        │
        ▼
kube-proxy -> поды gateway -> HTTPRoute/GRPCRoute backends
```

easyLB отвечает только за L4-адреса data-plane Service. L7-роутинг остаётся
ответственностью реализации Gateway API.

## Поддерживаемые версии

- easyLB смотрит `Gateway` только через `gateway.networking.k8s.io/v1`. Подойдёт
  любой бандл Gateway API, который отдаёт `v1` `Gateway` (v1.0.0 и новее).
  Кластеры, где есть только `v1beta1`/`v1alpha2` Gateway CRD, нативной
  интеграцией не поддерживаются — создавай `VIPBinding` вручную.
- Controller работает через `unstructured` и читает только
  `spec.addresses[].value` для записей с `type: IPAddress` и аннотации
  `easylb.io/*`. Новые поля, новые типы адресов и новые ресурсы игнорируются,
  а не ломают reconcile.
- Новый релиз Gateway API может требовать новый Kubernetes. Gateway API
  **v1.5+** использует `ValidatingAdmissionPolicy` и CEL `isIP` и требует
  Kubernetes **1.31+**. На старых кластерах ставь **v1.4.1** (или v1.0.0,
  официально поддерживающую k8s 1.25), если она отдаёт `v1` API.
- Новые ресурсы Gateway API (`ListenerSet`, `BackendTLSPolicy`, `TLSRoute`,
  `XBackend` и т.п.) easyLB не интерпретирует. Они могут работать внутри
  реализации Gateway; easyLB управляет только data-plane `Service`.
- Поддерживаются все реализации, которые создают `Service` типа
  `LoadBalancer` для data-plane (Envoy Gateway, NGINX Gateway Fabric, Cilium,
  Traefik, Kong, Istio, ...). Implementation-специфичного кода нет.

Controller'у нужен `controller.requireClass: false`, если data-plane Service
без `loadBalancerClass` (обычный случай для Gateway-реализаций). При
`requireClass: false` easyLB берёт **все** `LoadBalancer` Service без класса,
поэтому используй это только если easyLB — единственный такой controller в
кластере, либо задай класс на data-plane Service.

## Тестирование без кластера

Логика Gateway → `VIPBinding` покрыта синтетическими unit-тестами, которым не
нужны Gateway API CRD и кластер:

```bash
go test ./internal/controller -run Gateway -v
```

Тесты собирают `Gateway` в форме релизов Gateway API v1.0, v1.2, v1.4, v1.5 и
v1.7, добавляют неизвестные будущие поля и ресурсы и проверяют, что easyLB
по-прежнему создаёт правильный binding, соблюдает приоритет
адресов/пула, игнорирует `status.addresses`, не перезаписывает чужой binding и
отклоняет неверный `vip-mode`. Это проверяет логику controller'а, но не
реальную схему CRD, admission и conversion — после обновления
Kubernetes/Gateway API всё равно прогони end-to-end проверку на кластере.

## Порядок интеграции

1. Установить реализацию Gateway API и создать `Gateway`.
2. Найти data-plane `Service` типа `LoadBalancer`, который создала реализация.
   Имя зависит от реализации.
3. Создать `VIPBinding` на этот Service — так же, как для ingress-nginx.
4. Оставить `externalTrafficPolicy: Local` на data-plane Service, если нужен
   source IP клиента.
5. Убедиться, что у Gateway data plane есть локальный под на каждой ноде,
   которая может владеть VIP, либо использовать `Cluster` и принять SNAT.

Минимальный `VIPBinding`:

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: gateway-keepalived
  namespace: gateway-system
spec:
  serviceRef:
    name: <data-plane-service>
  mode: external
  vips:
    - 192.168.56.250
```

Или с пулом и L2-speaker:

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPBinding
metadata:
  name: gateway-easylb
  namespace: gateway-system
spec:
  serviceRef:
    name: <data-plane-service>
  mode: l2
  poolRef:
    name: gateway-vips
```

То же самое через Helm:

```yaml
bindings:
  gateway-easylb:
    namespace: gateway-system
    serviceRef: <data-plane-service>
    mode: l2
    poolRef: gateway-vips
```

## Нативная интеграция (опционально)

Вместо ручного создания `VIPBinding` можно позволить easyLB преобразовать
`Gateway` в `VIPBinding`.

Включение:

1. Установить Gateway API **v1** CRD (`gateway.networking.k8s.io/v1`).
2. Включить `gatewayAPI.enabled: true` в Helm (или флаг
   `--enable-gateway-api` у controller'а).
3. Добавить аннотации на `Gateway`:

| Аннотация | Обязательна | Значение |
|---|---|---|
| `easylb.io/service` | да | имя data-plane `Service` |
| `easylb.io/vip-mode` | нет | `external` (по умолчанию) или `l2` |
| `easylb.io/vip-pool` | нет | имя `VIPPool`, если IP не заданы |
| `easylb.io/vips` | нет | fallback-список VIP через запятую |

4. IP-адреса указывать в `Gateway.spec.addresses` с `type: IPAddress`. Если
   адресов нет, используются `easylb.io/vip-pool` или `easylb.io/vips`.

Пример с явным адресом:

```yaml
apiVersion: gateway.networking.k8s.io/v1
kind: Gateway
metadata:
  name: public
  namespace: gateway-system
  annotations:
    easylb.io/service: envoy-public-abc123
    easylb.io/vip-mode: l2
spec:
  gatewayClassName: envoy-gateway
  addresses:
    - type: IPAddress
      value: 192.168.56.240
  listeners:
    - name: https
      protocol: HTTPS
      port: 443
```

Пример с пулом:

```yaml
metadata:
  annotations:
    easylb.io/service: envoy-public-abc123
    easylb.io/vip-mode: l2
    easylb.io/vip-pool: gateway-vips
```

Controller создаёт `VIPBinding` с именем `gateway-<gateway-name>` в namespace
Gateway и ownerReference на Gateway. Binding обновляется при изменении Gateway
и удаляется сборщиком мусора при удалении Gateway. Дальше работает обычная
цепочка: `VIPBinding` controller выделяет адрес, Service controller публикует
его, speaker анонсирует при `mode: l2`.

Важно:

- без аннотации `easylb.io/service` binding не создаётся;
- `Gateway.status.addresses` easyLB не пишет;
- если реализация Gateway сама владеет `spec.addresses`, не включай для неё
  интеграцию easyLB — один VIP, один владелец;
- если включить `gatewayAPI.enabled` без установленных Gateway API v1 CRD,
  controller не запустится: watch на Gateway не сможет синхронизироваться.

## Как найти data-plane Service

Универсального label для всех реализаций нет. На практике:

- Envoy Gateway обычно создаёт Service с именем вида
  `envoy-<gateway-name>-<hash>`;
- Cilium Gateway использует `cilium-gateway-<gateway-name>`;
- NGINX Gateway Fabric использует `<gateway-name>-nginx`.
- Kong, Traefik и Istio создают свои implementation-specific имена.

Смотри namespace Gateway:

```bash
kubectl get svc -n <gateway-namespace> -o wide
kubectl get svc -n <gateway-namespace> -o jsonpath='{range .items[?(@.spec.type=="LoadBalancer")]}{.metadata.name}{"\n"}{end}'
```

Проверь, что selector указывает на поды gateway:

```bash
kubectl -n <gateway-namespace> get svc <data-plane-service> -o yaml
kubectl -n <gateway-namespace> get endpoints <data-plane-service>
```

Если реализация позволяет задать аннотации или фиксированное имя Service
(например, через `EnvoyProxy` или `NginxProxy`), лучше зафиксировать имя,
чтобы оно не менялось при обновлениях.

## Gateway.spec.addresses и VIPBinding

На один VIP должен быть один владелец:

- если реализация сама умеет `Gateway.spec.addresses` и публикует адрес,
  easyLB для этого Gateway может быть не нужен;
- если реализация ожидает внешний Service LB controller — используй
  `VIPBinding` и **не** задавай одновременно `Gateway.spec.addresses`;
- не настраивай оба механизма сразу: они начнут спорить за адрес или дадут
  противоречивый статус.

При включённой нативной интеграции easyLB читает IP-значения из
`Gateway.spec.addresses`, но никогда не пишет `Gateway.spec.addresses` и
`Gateway.status.addresses`.

## Source IP

Для сохранения source IP data-plane Service должен использовать
`externalTrafficPolicy: Local`. Это работает только если на ноде, куда пришёл
трафик, есть Ready endpoint gateway.

- DaemonSet-подобный data plane (под на каждой ноде) — самый простой вариант.
- Deployment с несколькими репликами может оставить ноды без локального
  endpoint; при `Local` трафик на VIP на таких нодах будет дропаться.
- Для `mode: l2` easyLB speaker выбирает лидером только ноду с локальным
  endpoint (при `Local`), поэтому VIP приезжает туда, где есть под gateway.
- Для `mode: external` keepalived может перевести VIP на ноду без пода gateway.
  Используй `vrrp_script` на `healthCheckNodePort` сервиса, чтобы понижать
  приоритет такой ноды (см. [source-ip.md](source-ip.md)).

## Проверка

```bash
kubectl get gateway -A
kubectl -n <gateway-namespace> get svc <data-plane-service> -o wide
kubectl get vipbinding -A
kubectl -n easylb-system logs ds/easylb-speaker --tail=100

curl -k --resolve example.com:443:<VIP> https://example.com/
```

Для L2 VIP `ping` не ответит; используй `arping` и TCP-проверки:

```bash
arping -c 3 <VIP>
nc -vz <VIP> 443
```

## Ограничения

- easyLB не реализует Gateway API, а управляет только data-plane Service.
- Нет enforcement `loadBalancerSourceRanges`.
- IPv6 NDP для speaker пока не реализован.
- BGP пока нет.
- Имя data-plane Service может измениться при обновлении реализации Gateway;
  после апгрейда проверяй `VIPBinding`.
- Один VIP — один владелец. Нельзя одновременно держать один VIP на
  keepalived, easyLB `mode: l2`, MetalLB и собственном LB реализации Gateway.
