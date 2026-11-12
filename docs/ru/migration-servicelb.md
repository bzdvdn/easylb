# Миграция с ServiceLB (klipper)

В k3s/RKE2 встроен ServiceLB (klipper). Он создаёт `svclb`-поды с `hostPort` и
DNAT+MASQUERADE: появляется лишний хоп, а client source IP может теряться.
easyLB заменяет этот control plane, не проксируя пакеты: kube-proxy ставит
правила по адресам, которые публикует easyLB.

Гайд покрывает оба режима:

- только IP нод — ближе всего к ServiceLB, без VIP;
- стабильные VIP — L2 speaker easyLB или внешний владелец VIP.

## 1. Отключи ServiceLB

На каждой server-ноде k3s/RKE2:

```yaml
# /etc/rancher/k3s/config.yaml
disable:
  - servicelb
```

```bash
sudo systemctl restart k3s
```

После рестарта существующие `svclb`-поды удаляются, а `LoadBalancer` Service
временно остаются без внешнего адреса. Ставь easyLB в это же окно, чтобы
сократить простой.

## 2. Установи easyLB

```bash
helm upgrade --install easylb oci://ghcr.io/bzdvdn/charts/easylb \
  --version <chart-version> \
  --namespace easylb-system \
  --create-namespace \
  -f values-easylb.yaml
```

### Вариант A: только IP нод

Минимальный шаг от ServiceLB. easyLB публикует IP нод в статусе Service, а
kube-proxy делает остальное.

```yaml
controller:
  publishNodeIPs: true
  requireClass: false

speaker:
  enabled: false

pools: {}
bindings: {}
```

DNS продолжает указывать на IP нод. VIP и speaker не нужны.

### Вариант B: стабильные L2 VIP

```yaml
controller:
  publishNodeIPs: false   # true, если DNS всё ещё использует IP нод
  requireClass: false

speaker:
  enabled: true

pools:
  ingress-vips:
    addresses:
      - 192.168.56.240-192.168.56.249
    autoAssign: true
    mode: l2
    serviceAllocation:
      namespaceSelector:
        matchLabels:
          kubernetes.io/metadata.name: ingress-nginx
      serviceSelector:
        matchLabels:
          app.kubernetes.io/name: ingress-nginx
```

После появления `EXTERNAL-IP` обнови DNS на выделенный VIP.

### Вариант C: оставить существующий VIP

Если VIP уже держит keepalived или аппаратный балансировщик, L2 speaker не
включай. Создай явный binding:

```yaml
speaker:
  enabled: false

bindings:
  ingress-keepalived:
    namespace: ingress-nginx
    serviceRef: my-release-ingress-nginx-controller
    mode: external
    vips:
      - 192.168.56.250
```

## 3. Убери оставшиеся `svclb`-объекты

```bash
kubectl -n kube-system get ds | grep svclb || true
kubectl -n kube-system get pods | grep svclb || true
```

Если после отключения ServiceLB что-то осталось, удали соответствующие
DaemonSet'ы/поды. После отключения k3s ServiceLB controller они больше не
управляются.

## 4. Проверь

```bash
kubectl -n easylb-system get deploy,ds,pods
kubectl get vippool
kubectl get vipbinding -A
kubectl get svc -A --field-selector spec.type=LoadBalancer
kubectl -n easylb-system get lease
kubectl -n easylb-system logs ds/easylb-speaker --tail=100
```

Ожидаемо:

- `EXTERNAL-IP` снова заполнен (IP нод или VIP);
- `svclb`-подов нет;
- для `mode: l2` работают `arping`/`ip -6 neigh` и TCP-проверки; ICMP echo не
  отвечает;
- в логах ingress виден IP клиента при `externalTrafficPolicy: Local` и
  локальном ingress endpoint.

## 5. Откат

1. Удали easyLB (и его `VIPPool`/`VIPBinding`).
2. Убери `disable: [servicelb]` из конфига k3s и перезапусти k3s.
3. ServiceLB заново создаст `svclb`-поды для существующих `LoadBalancer`
   Service.

## Примечания

- Один VIP — один владелец. Не запускай easyLB L2 и keepalived на одном VIP.
- Режим IP нод не требует пула, binding'ов и speaker'а.
- При переходе с IP нод на VIP обнови DNS и поставь `publishNodeIPs: false`,
  чтобы публиковались только VIP.
- Для MetalLB смотри [migration-metallb.md](migration-metallb.md).
