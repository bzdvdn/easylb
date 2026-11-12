# Troubleshooting

[English](../en/troubleshooting.md) | **Русский**

## Первые проверки

```bash
kubectl -n easylb-system get deploy,ds,pods -o wide
kubectl get vippool
kubectl get vipbinding -A -o wide
kubectl get vipbinding -A -o yaml
kubectl -n ingress-nginx get svc my-release-ingress-nginx-controller -o wide
kubectl -n easylb-system get lease
```

Дерево проверок:

1. VIP есть в `Service.status.loadBalancer.ingress`?
   - Нет: проблема controller/status.
   - Да: идём дальше.
2. `VIPBinding.status.allocatedIPs` корректны и `Ready=True`?
   - Нет: аллокация/конфликт/пул.
   - Да: идём дальше.
3. Для `mode: l2` speaker анонсирует?
   - Смотри логи: `advertising VIPs`.
   - Смотри Lease holder.
4. ARP резолвится?
   - `arping -c 3 <VIP>`.
   - Нет: speaker/ARP/права/интерфейс.
   - Да: идём дальше.
5. TCP работает?
   - `nc -vz <VIP> 443`, `curl`.
   - Нет: kube-proxy/status/обратный маршрут.
   - Да: всё в порядке.

## VIP нет в EXTERNAL-IP

- Логи controller:

  ```bash
  kubectl -n easylb-system logs deploy/easylb-controller --tail=200
  ```

- Conditions `VIPBinding` (`Ready`, `reason`).
- `publishNodeIPs`/`nodeIPTypes`, если пропали IP нод.
- Если у Service чужой `loadBalancerClass`, controller его игнорирует:

  ```bash
  kubectl -n ingress-nginx get svc ... -o jsonpath='{.spec.loadBalancerClass}{"\n"}'
  ```

- Убедись, что штатный ServiceLB k3s/RKE2 выключен (`--disable servicelb`).

## VIPBinding не Ready

| Reason | Что делать |
|---|---|
| `PoolNotFound` | создать `VIPPool` или исправить `poolRef` |
| `PoolExhausted` | добавить адреса или освободить binding |
| `Conflict` | другой binding занял тот же VIP |
| `InvalidSpec` | либо `vips`, либо `poolRef`, не оба |
| `InvalidVIP` | исправить IP |
| `RequestedIPUnavailable` | выбрать другой IP или освободить его |
| `PoolAutoAssignDisabled` | включить `autoAssign` или задать `requestedIP` |

## Speaker не ready / crashloop

```bash
kubectl -n easylb-system describe pod -l app.kubernetes.io/component=speaker
kubectl -n easylb-system logs ds/easylb-speaker --tail=200
```

Частые причины:

- `operation not permitted` / raw socket: speaker не root или нет `NET_RAW`.
  Проверь:

  ```bash
  kubectl -n easylb-system get ds easylb-speaker \
    -o jsonpath='{.spec.template.spec.securityContext}{"\n"}'
  ```

  Должно быть `runAsUser: 0`. Если включён PSA — разреши namespace
  (`pod-security.kubernetes.io/enforce=privileged`).

- `no interface owns address`: `InternalIP` ноды не на интерфейсе или неверный
  `nodeIPType`. Задай `speaker.interface` явно или исправь IP ноды.
- Ошибки Lease/leader election: проверь RBAC на Leases и namespace
  `easylb-system`.
- Конфликт портов `8082`/`8083` в hostNetwork: проверь ноду.

## `VIPAlreadyAssigned`

VIP уже назначен на интерфейс, обычно keepalived. Для `mode: l2` это конфликт:
один VIP — один владелец. Убери VIP из keepalived или переведи binding в
`mode: external`.

## ARP не резолвится

```bash
arping -c 3 <VIP>
ip neigh show <VIP>
```

На ноде, которая держит Lease:

```bash
sudo tcpdump -ni <iface> arp
ip -br a | grep <VIP>
```

- ARP-запроса вообще нет: маршрутизация клиента/VLAN.
- Запрос приходит, ответа нет: speaker не лидер, ARP responder упал или не тот
  интерфейс.
- Ответ есть, но клиент использует старый MAC: очисти ARP-кэш
  (`ip neigh flush <VIP>`).

## ARP работает, TCP нет

- Проверь правила kube-proxy:

  ```bash
  iptables-save | grep <VIP>
  # или
  ipvsadm -Ln | grep <VIP>
  ```

- Нет правила — VIP не в статусе Service или kube-proxy не синхронизировался.
- Проверь endpoints backend-Service.
- Проверь `externalTrafficPolicy: Local` и локальный endpoint на владельце VIP.
- Проверь обратный маршрут, если клиент не в той же L2.

## `ping` по l2 VIP не работает

Это нормально. VIP не назначается на интерфейс, speaker отвечает только на
ARP. Используй `arping` и TCP-проверки.

## Stale Lease holder

Lease держит несуществующий под. Текущий speaker при graceful shutdown
освобождает Lease сразу, при hard crash — через `leaseDuration` (15s). Если
Lease старее, удали его:

```bash
kubectl -n easylb-system delete lease <lease-name>
```

Текущий speaker создаст его заново.

## CRD / Helm

- `no matches for kind "VIPBinding"`: установи `deploy/crds.yaml` или Helm-чарт.
- `helm upgrade` не обновляет CRD из `crds/`; применяй отдельно.
- Конфликт ownership со статическими манифестами: удали ресурсы
  `deploy/easylb.yaml` и `deploy/easylb-speaker.yaml` перед Helm.

## Gateway API

- `gatewayAPI.enabled=true` без Gateway API v1 CRD: controller не стартует.
  Установи CRD или выключи функцию.
- Установка CRD падает с `no matches for kind "ValidatingAdmissionPolicy"`
  или `undeclared reference to 'isIP'` → релиз Gateway API слишком новый для
  кластера. v1.5+ требует Kubernetes 1.31+. Удали частично установленные CRD
  `gateway.networking.k8s.io`, поставь v1.4.1 или обнови Kubernetes.
- `VIPBinding` не создаётся: проверь аннотацию `easylb.io/service`,
  `Gateway.spec.addresses` и что controller реально запущен с
  `--enable-gateway-api=true`.
- `VIPBinding` есть, а VIP в Service нет: data-plane Service без
  `loadBalancerClass`, а `controller.requireClass: true`. Поставь
  `requireClass: false` или задай класс на Service.
- `BindingConflict` на Gateway: `VIPBinding gateway-<name>` уже существует и
  принадлежит другому Gateway.
- `Gateway` не `Programmed`: проверь `GatewayClass`, затем логи реализации
  (например, `kubectl -n envoy-gateway-system logs deploy/envoy-gateway`).
- При `externalTrafficPolicy: Local` speaker не выбирает лидером ноды без
  локального endpoint gateway. Проверь EndpointSlice и число реплик data plane.
- Имя data-plane Service изменилось после обновления Gateway: обнови
  аннотацию.
- Реализация Gateway сама владеет `spec.addresses`: выбери одного владельца.

## Полезные команды

```bash
kubectl -n easylb-system logs deploy/easylb-controller --tail=200
kubectl -n easylb-system logs ds/easylb-speaker --tail=200
kubectl -n easylb-system get lease -o yaml
kubectl get vipbinding -A -o yaml
kubectl -n ingress-nginx get endpoints <service>
kubectl get events -A --sort-by=.lastTimestamp | tail -30
```
