# easyLB

[English](README.md) | **Русский**

Лёгкий Kubernetes LoadBalancer controller для bare-metal, on-prem,
k3s и RKE2-кластеров. Публикует IP нод и VIP в
`Service.status.loadBalancer.ingress`, а весь dataplane оставляет штатному
**kube-proxy** — без лишних подов, NAT-хопа и проксирования пакетов.

Это полноценная замена ServiceLB в k3s/RKE2 и L2-режима MetalLB для небольших
bare-metal кластеров, которым не нужен BGP и продвинутый traffic engineering
из MetalLB.

Можно подключить VIP, которым ты уже владеешь (keepalived/VRRP, аппаратный
балансировщик, другой controller), и easyLB просто опубликует его. А можно
отдать VIP easyLB: L2-speaker с ARP и leader election на каждый VIP сделает
полноценный отказоустойчивый балансировщик без BGP и внешних зависимостей.

## Зачем это нужно

В on-prem Kubernetes обычно нет облачного балансировщика. Штатный ServiceLB в
k3s/RKE2 (klipper) закрывает эту нишу `svclb`-подами с `hostPort` и
DNAT+MASQUERADE: появляется лишний хоп, а source IP клиента может теряться.

easyLB идёт по control-plane пути: записывает нужные адреса в статус
Service, а правила DNAT ставит kube-proxy:

```text
клиент -> NodeIP или VIP -> kube-proxy -> ingress-nginx / pod
                              ^
                    правила KUBE-EXT по адресам из status
```

При `externalTrafficPolicy: Local` и локальном Ready endpoint source IP
клиента сохраняется, а `svclb`-поды и лишний NAT не нужны. Один и тот же
Service может использовать как существующий VIP, так и L2 VIP под
управлением easyLB.

## Для кого

easyLB рассчитан на небольшие и средние **bare-metal / on-prem** кластеры,
где:

- нет облачного балансировщика, но нужен `Service` типа `LoadBalancer`;
- DNS указывает на IP нод и/или стабильные VIP;
- уже есть VIP (keepalived/VRRP, аппаратный LB) и хочется использовать его в
  Kubernetes без дополнительного прокси;
- или нужен полноценный HA L2 VIP без BGP, MetalLB и keepalived;
- ingress-nginx или data plane Gateway API работает как Service/DaemonSet;
- важен client source IP (`externalTrafficPolicy: Local`).

easyLB **не** предназначен для:

- public cloud/NAT-сценариев, где нужен hostPort-fallback;
- `loadBalancerSourceRanges` enforcement;
- BGP/ECMP, DSR, L7/TLS termination;
- возможностей MetalLB сверх L2 (BGP, кастомные анонсы, продвинутые traffic
  policies);
- автоматического управления keepalived/VRRP.

## Возможности

- Замена ServiceLB в k3s/RKE2: без `svclb`-подов, hostPort и лишнего NAT.
- Замена L2-режима MetalLB для небольших bare-metal кластеров: IPAM через
  `VIPPool`, ARP-анонс, leader election на каждый VIP и failover без BGP.
- Автоаллокация в стиле MetalLB: подходящие `LoadBalancer` Service получают
  `VIPBinding` и VIP без ручного создания CR.
- Публикация IP нод и VIP в `status.loadBalancer.ingress`.
- Режим `external` для VIP, которыми владеешь ты: keepalived/VRRP,
  аппаратный LB или любой внешний адрес.
- Режим `l2`: VIP принадлежит easyLB, speaker анонсирует IPv4 через ARP,
  IPv6 через NDP, а leader election и failover работают через Kubernetes
  Lease.
- Автоматическая аллокация из `VIPPool` или явные адреса в `VIPBinding`.
- HA controller: несколько реплик, leader election, PDB, spread по нодам,
  optimistic lock при записи статуса.
- Prometheus-метрики и опциональный ServiceMonitor.
- Сохранение client source IP при `externalTrafficPolicy: Local`.
- Конфигурация только через CRD, без аннотаций на ingress/gateway Service.
- Опциональная нативная интеграция с Gateway API: `Gateway` превращается в
  `VIPBinding`.
- Helm-чарт, публикуемый как OCI-артефакт.

## Режимы VIP

| Режим | Кто владеет VIP | Speaker | ICMP ping | Когда использовать |
|---|---|---|---|---|
| `external` | keepalived/VRRP, аппаратный LB или админ | нет | да, если адрес назначен | подключить существующий VIP; easyLB только публикует статус |
| `l2` | easyLB speaker | да | нет (только ARP/NDP) | полноценный HA LB без keepalived, BGP и MetalLB |
| `bgp` | зарезервировано | будущее | — | будущий BGP-режим |

Оба режима можно смешивать на одном Service: один binding для существующего
keepalived VIP, второй — для L2 VIP под управлением easyLB.

Для `l2` VIP **не назначается на интерфейс**: speaker отвечает на ARP, а
TCP/HTTP обрабатывает kube-proxy. Поэтому `ping` по такому VIP не работает —
это нормальное поведение (аналогично MetalLB L2), используйте TCP-проверки.

## Быстрый старт

### 1. Образ

```bash
docker build -t <registry>/easylb:<tag> .
docker push <registry>/easylb:<tag>
```

### 2. Helm

```bash
helm upgrade --install easylb charts/easylb \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml
```

Опубликованный чарт также доступен как OCI-артефакт:

```bash
helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml
```

Минимальный `my-values.yaml`:

```yaml
controller:
  image:
    repository: <registry>/easylb
    tag: <tag>
  publishNodeIPs: true      # false, если DNS ходит только на VIP

speaker:
  enabled: true
  image:
    repository: <registry>/easylb
    tag: <tag>

pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true

bindings:
  ingress-easylb:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: l2
    poolRef: ingress-vips
  # Опционально: опубликовать VIP, которым ты уже владеешь.
  # Подходит keepalived/VRRP, аппаратный LB или любой внешний адрес.
  ingress-external:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
```

Готовый пример: `charts/easylb/values-example.yaml`.

Можно вообще не задавать `bindings`: пул с `autoAssign: true` сам создаст
`auto-<service>` bindings для подходящих `LoadBalancer` Service, как MetalLB.
См. [автоматическую аллокацию](docs/ru/configuration.md#auto-allocation).

### 3. Проверка

```bash
kubectl -n easylb-system get deploy,ds,pods
kubectl get vippool
kubectl get vipbinding -A
kubectl -n easylb-system get lease
kubectl -n ingress-nginx get svc my-release-ingress-nginx-controller -o wide
```

В `EXTERNAL-IP` должны появиться VIP. Для `l2` проверяйте доступность через
TCP:

```bash
arping -c 3 192.168.56.240
curl -k -H 'Host: echo1.local' https://192.168.56.240/
```

## Конфигурация

Источник истины — CRD:

```yaml
apiVersion: easylb.io/v1alpha1
kind: VIPPool
metadata:
  name: ingress-vips
spec:
  addresses:
    - 192.168.56.240-192.168.56.249
  autoAssign: true
---
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

Аннотации на Service (`easylb.io/vips`, `easylb.io/vip-config`) больше не
используются. Подробнее: [docs/ru/configuration.md](docs/ru/configuration.md).

## Source IP

Кратко:

- `externalTrafficPolicy: Local` + локальный Ready endpoint + kube-proxy =
  client source IP сохраняется.
- `Cluster` может сделать SNAT при пересылке на другую ноду; source IP не
  гарантируется.
- Для `l2` speaker выбирает лидером только ноду с локальным endpoint (при
  `Local`), поэтому VIP приезжает туда, где есть ingress.
- Для `external` VIP ответственность за health/failover несёт внешний
  владелец (keepalived, аппаратный LB или другая система); на ноде-владельце
  должен быть локальный ingress-под.

Подробнее: [docs/ru/source-ip.md](docs/ru/source-ip.md).

## Gateway API

Реализации Gateway API (Envoy Gateway, NGINX Gateway Fabric, Cilium, Traefik,
Kong, Istio, ...) поднимают data-plane как `Service` типа `LoadBalancer`.
easyLB управляет этим Service так же, как ingress-nginx. При
`gatewayAPI.enabled: true` controller дополнительно смотрит `Gateway` в
стабильном API `gateway.networking.k8s.io/v1` и сам создаёт `VIPBinding` из
`Gateway.spec.addresses` и нескольких аннотаций.

easyLB не зависит от конкретной реализации или релиза Gateway API: он
работает через `unstructured` и читает только стабильную поверхность `v1`,
поэтому новые поля и ресурсы игнорируются, а не ломают reconcile. Gateway API
v1.5+ требует Kubernetes 1.31+; на старых кластерах используй v1.4.1 (или
v1.0.0 для Kubernetes 1.25), если CRD отдают `v1` API. ListenerSet, TLSRoute,
запись статуса Gateway и другие новые фичи easyLB не реализует.

Подробнее: [docs/ru/gateway-api.md](docs/ru/gateway-api.md).

## Отказоустойчивость

- Controller: 2 реплики, leader election, PDB, spread по нодам. Падение
  лидера не останавливает трафик; standby берёт Lease.
- Speaker: Lease на каждый `VIPBinding`, ARP/gratuitous ARP; при graceful
  shutdown Lease освобождается сразу, при crash — по истечении
  `leaseDuration` (15s).
- kube-proxy/conntrack остаются штатным dataplane. Установленные TCP-сессии
  рвутся при смене владельца VIP или падении backend-поды — это ограничение
  любого L4 VIP, а не easyLB.
- Если DNS использует IP нод, `publishNodeIPs` должен быть `true`; при
  `false` публикуются только VIP.

## Ограничения

- нет `loadBalancerSourceRanges`;
- нет hostPort/NAT-fallback для public cloud;
- `mode: bgp` зарезервирован, но не реализован;
- CRD из Helm `crds/` не обновляются при `helm upgrade`;
- speaker требует `hostNetwork`, root и `NET_RAW`; при PSA namespace должен
  разрешать это (например, `pod-security.kubernetes.io/enforce=privileged`);
- l2 VIP не отвечает на ICMP;
- easyLB не проверяет конфликт с внешним владельцем VIP. Один VIP — один
  владелец, это ответственность конфигурации.

## Документация

Собранная документация: <https://bzdvdn.github.io/easylb/>

- [Helm-чарт](charts/easylb/README.md)
- [Архитектура](docs/ru/architecture.md)
- [Конфигурация](docs/ru/configuration.md)
- [Source IP](docs/ru/source-ip.md)
- [Эксплуатация и HA](docs/ru/operations.md)
- [Troubleshooting](docs/ru/troubleshooting.md)
- [Сравнение с ServiceLB/MetalLB/kube-vip](docs/ru/comparison.md)
- [Gateway API](docs/ru/gateway-api.md)
- [Миграция с MetalLB](docs/ru/migration-metallb.md)
- [Миграция с ServiceLB](docs/ru/migration-servicelb.md)
- [Roadmap](ROADMAP.md)
- [Примеры](examples/README.md)
- [Архитектурные решения](docs/ru/decisions/)

## Разработка

```bash
make build          # бинари controller и speaker
make test
make vet
make lint           # golangci-lint (revive, staticcheck, gosec, ...)
make sec            # отдельный gosec-скан
make docker-build IMAGE=<registry>/easylb:<tag>
make helm-lint
make helm-template
make e2e             # k3s/k3d smoke test (test/e2e/run.sh)
```

Структура:

```text
cmd/controller/        easyLB controller
cmd/speaker/           L2 speaker
api/v1alpha1/          VIPPool, VIPBinding
internal/controller/   Service + VIPBinding контроллеры
internal/speaker/      Lease, ARP, eligibility, interface
internal/ipam/         разбор пулов и аллокация
internal/vip/           модель VIP и источник из VIPBinding
deploy/                статические манифесты и CRD
charts/easylb/         Helm chart
```

## Roadmap

Полный статус, сделанное и план фич: [ROADMAP.md](ROADMAP.md).

Текущий фокус: per-pool селекторы L2-анонса, e2e для shared VIP/IPv6 NDP и
готовые алерты. BGP/BFD/ECMP остаются вне scope.
