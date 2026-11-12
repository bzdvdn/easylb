# Source IP

[English](../en/source-ip.md) | **Русский**

Сохранение client source IP определяется не easyLB, а тем, как пакет доходит до
backend-поды: DNAT, SNAT, маршрутизацией и `externalTrafficPolicy`.

## Матрица

| Вход | Политика | Что видит ingress | Source IP |
|---|---|---|---|
| NodeIP | `Local` + локальный endpoint | реальный клиент | сохраняется |
| NodeIP | `Cluster` | IP ноды/шлюза (возможен SNAT) | не гарантируется |
| External VIP (keepalived) | `Local` + endpoint на владельце VIP | реальный клиент | сохраняется |
| External VIP (keepalived) | `Cluster` | возможен SNAT | не гарантируется |
| L2 VIP (speaker) | `Local` + endpoint на лидере | реальный клиент | сохраняется |
| L2 VIP (speaker) | `Cluster` | возможен SNAT | не гарантируется |

## Почему при keepalived source IP терялся в klipper

k3s ServiceLB ставит `svclb`-поды с `hostPort`. kube-proxy знает только
адреса из `status.loadBalancer.ingress` (обычно IP нод). VIP keepalived туда
не входил, поэтому:

```text
клиент -> VIP -> hostPort -> svclb pod
                              DNAT + MASQUERADE (в netns пода)
                              -> ingress видит IP svclb-пода
```

easyLB публикует VIP в статусе. kube-proxy получает адрес и делает DNAT сам,
без промежуточного пода и без MASQUERADE для внешнего клиента при `Local`.

## Требования для Local

1. `externalTrafficPolicy: Local` на Service.
2. На ноде, куда пришёл трафик, есть Ready endpoint этого Service.
   Для ingress-nginx это обычно DaemonSet на всех нодах.
3. Для `l2` speaker выбирает лидером только ноду с локальным Ready endpoint
   (при `Local`).
4. Для `external` VIP keepalived должен держать VIP на ноде с локальным
   endpoint. Иначе при `Local` трафик будет дропаться.

## Keepalived: health check на локальный endpoint

У Service с `externalTrafficPolicy: Local` есть `healthCheckNodePort`, который
kube-proxy обслуживает на ноде и который отвечает 200 только при наличии
локального endpoint. Его удобно использовать в `vrrp_script`:

```conf
vrrp_script chk_ingress {
    script "/usr/bin/curl -sf http://127.0.0.1:<healthCheckNodePort>/healthz"
    interval 2
    fall 3
    rise 2
    weight -20
}

vrrp_instance VI_INGRESS {
    state BACKUP
    interface enp0s8
    virtual_router_id 51
    priority 150
    advert_int 1
    nopreempt
    unicast_src_ip 192.168.56.142
    unicast_peer {
        192.168.56.143
    }
    virtual_ipaddress {
        192.168.56.250/32 dev enp0s8
    }
    track_script {
        chk_ingress
    }
}
```

Если локальный ingress нездоров, приоритет падает, и VIP уезжает на ноду со
здоровым endpoint.

`healthCheckNodePort` можно посмотреть так:

```bash
kubectl -n ingress-nginx get svc my-release-ingress-nginx-controller \
  -o jsonpath='{.spec.healthCheckNodePort}{"\n"}'
```

## L2 VIP и ICMP

`mode: l2` VIP не назначается на интерфейс. Speaker отвечает на ARP (IPv4) и
NDP (IPv6).
Поэтому:

- `ping` по VIP не работает — это нормально;
- `arping` работает для IPv4; для IPv6 используй `ip -6 neigh` и TCP-проверки;
- TCP/UDP через kube-proxy работает;
- health-checkи делайте TCP (`curl`, `/healthz`, `nc`), не ICMP.

Это та же модель, что у MetalLB L2.

## DNS и failover

- DNS на IP нод не знает о падении ноды. Для HA используйте VIP.
- При смене владельца VIP установленные TCP-сессии рвутся: новый узел не
  знает старый conntrack. Клиенты должны переподключиться.
- Для keepalived используйте VRRP-приоритеты и health script.
- Для `l2` speaker используйте Lease и, при необходимости, более короткие
  тайминги (`lease-duration`, `renew-deadline`, `retry-period`).

## Проверка

С клиента:

```bash
curl -k -H 'Host: echo1.local' https://192.168.56.250/
curl -k -H 'Host: echo1.local' https://192.168.56.240/
arping -c 3 192.168.56.240
ip neigh show 192.168.56.240
```

В логах ingress-nginx:

```bash
kubectl -n ingress-nginx logs ds/my-release-ingress-nginx-controller --tail=100
```

Для L4-проверки:

```bash
sudo tcpdump -ni <iface> host 192.168.56.240 and port 443
```

Если в ingress-логе виден IP клиента — source IP сохранён. Если виден IP ноды
или пода `svclb` — проверьте `externalTrafficPolicy`, локальный endpoint и
режим VIP.
