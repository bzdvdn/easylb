# Эксплуатация и HA

[English](../en/operations.md) | **Русский**

## Установка, обновление, удаление

```bash
# установка
helm upgrade --install easylb charts/easylb \
  --namespace easylb-system \
  --create-namespace \
  -f my-values.yaml

# обновление
helm upgrade easylb charts/easylb \
  --namespace easylb-system \
  -f my-values.yaml

# удаление (CRD остаются)
helm uninstall easylb -n easylb-system
```

Важно:

- CRD из `charts/easylb/crds/` устанавливаются при первом `helm install` и
  **не обновляются** при `helm upgrade`. При изменении схемы применяй
  `deploy/crds.yaml` отдельно.
- `helm uninstall` не удаляет CRD. Удаление CRD удалит все
  `VIPPool`/`VIPBinding`.
- Если раньше ставил статические манифесты `deploy/`, удали их перед Helm,
  иначе будет конфликт ownership.

### Что удаление чистит, а что нет

easyLB сам не ставит iptables/ipvs-правила: их создаёт kube-proxy по
`Service.status.loadBalancer.ingress`. Это важно при удалении релиза.

`helm uninstall easylb -n easylb-system` удаляет:

- Deployment controller'а и DaemonSet speaker'а;
- ServiceAccount'ы, ClusterRole/ClusterRoleBinding и PDB;
- `VIPPool`/`VIPBinding`, отрендеренные из `pools`/`bindings` чарта;
- Lease leader election controller'а (освобождается при graceful shutdown).

Не удаляет:

- CRD из `charts/easylb/crds/`;
- `VIPPool`/`VIPBinding`, созданные вне релиза, включая `auto-<service>`
  bindings автоматической аллокации;
- namespace `easylb-system` (включая созданный через `--create-namespace`);
- уже опубликованные записи `Service.status.loadBalancer.ingress`;
- runtime-Lease, которые не удалились при graceful shutdown;
- iptables/ipvs-правила — ими владеет kube-proxy.

Что с трафиком после удаления:

- `mode: l2` VIP перестаёт отвечать на ARP сразу после остановки подов
  speaker'а. VIP в статусе Service и правила kube-proxy остаются, поэтому TCP
  может короткое время работать на нодах со старым neighbour cache, затем VIP
  становится недоступен.
- `mode: external` VIP продолжает работать, пока keepalived держит адрес и VIP
  остаётся в статусе Service: kube-proxy сохраняет DNAT-правила и без easyLB.
  Это застывшая конфигурация, а не поддерживаемое состояние.
- IP нод, опубликованные controller'ом, тоже остаются в статусе.

Полная очистка после удаления:

```bash
# Объекты, которые не входят в Helm-релиз.
kubectl get vipbinding -A
kubectl get vippool
kubectl delete vipbinding <name> -n <namespace>
kubectl delete vippool <name>

# Очистить опубликованные адреса, если Service не удаляется.
kubectl -n <namespace> patch svc <service> --type merge \
  -p '{"status":{"loadBalancer":{"ingress":[]}}}' --subresource=status

# Удалить застывшие runtime-Lease, если namespace остаётся.
kubectl -n easylb-system get lease
kubectl -n easylb-system delete lease <name>

# Опционально и деструктивно: удалит все VIPPool/VIPBinding.
kubectl delete crd vippools.easylb.io vipbindings.easylb.io
```

Поддерживаемый способ заставить speaker прекратить анонс и удалить per-VIP
Lease — удалить `VIPBinding`. Finalizer'а или cleanup-Job, который чистит
статус Service при uninstall, нет.

## HA controller

Чарт запускает 2 реплики:

- leader election (`Lease easylb.io` в `easylb-system`);
- `LeaderElectionReleaseOnCancel: true`;
- PDB `minAvailable: 1`;
- spread по `kubernetes.io/hostname`;
- rolling update `maxUnavailable: 0`, `maxSurge: 1`;
- optimistic lock при записи `status.loadBalancer.ingress` и
  `VIPBinding.status`.

Поведение при отказах:

| Событие | Последствие |
|---|---|
| Лидер упал | standby берёт Lease; статус не меняется |
| Graceful shutdown | Lease освобождается сразу |
| Hard crash | Lease истекает через `leaseDuration` (15s) |
| Все controller'ы лежат | трафик работает; статус застывает до возврата |
| API/etcd недоступны | reconcile стоит; статус не очищается |

## HA speaker

Speaker — DaemonSet, один под на ноду. Для каждого `mode: l2` VIPBinding
запускается leader election через отдельный `Lease` в `easylb-system`.

- Graceful shutdown освобождает Lease сразу (контекст привязан к signal
  handler).
- Crash обнаруживается через `leaseDuration` (15s по умолчанию).
- Новый лидер отправляет gratuitous ARP.
- VIP не назначается на интерфейс, поэтому упавший старый лидер не оставляет
  stale-адрес.

При failover установленные TCP-сессии рвутся; клиенты переподключаются. Это
свойство любого L4 VIP.

## Мониторинг

Controller (метрики controller-runtime на `:8080`):

```promql
leader_election_master_status{name="easylb.io"}
controller_runtime_reconcile_errors_total
controller_runtime_reconcile_total
controller_runtime_reconcile_time_seconds
```

Кастомные метрики controller'а:

- `easylb_controller_vip_allocations_total{result,reason}`;
- `easylb_controller_auto_bindings_total{action}`.

Speaker (метрики на `:8082`):

- `controller_runtime_reconcile_*` для контроллера `vipbinding`;
- логи: `advertising VIPs`, `stopped leading`,
  `failed to start ARP responder`, `failed to start NDP responder`,
  `VIPAlreadyAssigned`;
- `easylb_speaker_advertised_vips{node,binding}` — сколько VIP сейчас анонсируется;
- `easylb_speaker_leader_changes_total{node}`;
- `easylb_speaker_errors_total{interface,protocol,operation}`;
- Lease в `easylb-system` показывают текущего holder'а;
- с kube-state-metrics полезны `kube_lease_owner` и `kube_lease_renew_time`.

Чарт умеет создавать metrics-Service'ы и `ServiceMonitor` для Prometheus
Operator (`metrics.serviceMonitor.enabled: true`).

Полезные алерты:

- `leader_election_master_status{name="easylb.io"} != 1` дольше 30s;
- `rate(controller_runtime_reconcile_errors_total[5m]) > 0.1`;
- speaker DaemonSet не полностью ready:
  `kube_daemonset_status_number_ready{daemonset="easylb-speaker"} < kube_daemonset_status_desired_number_scheduled`;
- `VIPBinding` долго без `Ready=True`;
- Lease не обновляется дольше `leaseDuration` (kube-state-metrics).

Также следи за conntrack:

```bash
cat /proc/sys/net/netfilter/nf_conntrack_count
cat /proc/sys/net/netfilter/nf_conntrack_max
```

## Безопасность

- Controller: non-root, read-only root filesystem, drop ALL.
- Speaker: `hostNetwork`, root, `NET_RAW`; нужно для raw-сокетов ARP/NDP.
  Остальные capabilities сбрасываются.
- При Pod Security Admission namespace `easylb-system` должен разрешать
  speaker, например:

  ```bash
  kubectl label namespace easylb-system \
    pod-security.kubernetes.io/enforce=privileged --overwrite
  ```

- RBAC минимальный: Services, Nodes, EndpointSlices, VIPBindings, VIPPools,
  Leases, Events и Gateways (только при включённой Gateway-интеграции).
- Controller умеет create/update/delete `VIPBinding` из-за опциональной
  Gateway-интеграции. Ограничивай, кто может создавать `Gateway` в
  namespace, где включена эта функция.
- Валидируй VIP и пулы до применения; `VIPBinding` conditions показывают
  `Conflict`, `PoolExhausted`, `PoolNotFound`, `InvalidSpec`, `InvalidVIP`.

## Rollout и rollback

1. Собрать и запушить новый тег.
2. `helm upgrade` с новым тегом.
3. Controller: `maxUnavailable: 0` сохраняет лидера при обновлении.
4. Speaker: `maxUnavailable: 1` обновляет по одной ноде. Если обновляется
   под-лидер, VIP может переехать; established-сессии могут порваться.
5. Проверить:

   ```bash
   kubectl -n easylb-system rollout status deploy/easylb-controller
   kubectl -n easylb-system rollout status ds/easylb-speaker
   kubectl get vipbinding -A
   ```

6. Откат — `helm rollback`; при изменении CRD применяй предыдущую версию CRD
   отдельно.

## Ёмкость

- Controller: 2 небольшие реплики (requests 20m CPU / 32Mi, limits 200m / 128Mi).
- Speaker: маленький под на ноду (10m / 24Mi requests).
- `VIPPool` разворачивается максимум в 4096 адресов.
- Аллокатор — single-writer (`MaxConcurrentReconciles: 1`); для малых и
  средних кластеров этого достаточно.
- Для большого числа долгих сессий узкое место — kube-proxy/conntrack, а не
  easyLB.

## Runbook

### Failover лидера controller

```bash
kubectl -n easylb-system get lease easylb.io -o jsonpath='{.spec.holderIdentity}{"\n"}'
kubectl -n easylb-system get pods -o wide
```

Если лидера нет дольше ~30s — смотри логи controller и RBAC на Lease в
`easylb-system`.

### Failover speaker / VIP

```bash
kubectl -n easylb-system get lease -o wide
kubectl -n easylb-system logs ds/easylb-speaker --tail=200
arping -c 3 <VIP>
```

Если Lease держит несуществующий под и Lease старше `leaseDuration` — удали
Lease, текущий speaker создаст его заново.

### Failover keepalived

Keepalived вне easyLB. Проверяй VRRP, владельца VIP и `healthCheckNodePort`.
Для `Local` VIP должен быть на ноде с локальным ingress endpoint.

### Падение ноды

- Controller: standby на другой ноде продолжает.
- Speaker: другая нода берёт Lease и шлёт gratuitous ARP.
- NodeIP: controller убирает NotReady-адреса нод из статуса при reconcile.
- DNS: DNS на IP нод не знает об отказе; для HA используй VIP.
