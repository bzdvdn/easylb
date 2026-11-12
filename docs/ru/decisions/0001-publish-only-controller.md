# 0001 — Publish-only controller

**Статус:** Accepted

## Проблема

Трафик на keepalived VIP через k3s ServiceLB (klipper) проходит DNAT+MASQUERADE
внутри `svclb`-пода. Ingress видит IP пода, а не клиента. IP нод работают,
потому что kube-proxy перехватывает их раньше klipper.

## Ограничения

- kube-proxy есть на каждой ноде и уже умеет `status.loadBalancer.ingress`.
- Проект должен быть маленьким, безопасным и простым в установке.
- Keepalived остаётся владельцем VIP в существующих установках.

## Варианты

1. Форк klipper с изменением iptables-правил.
2. Собственный dataplane (ARP/NDP или прокси).
3. Публикация адресов в статус Service, dataplane — kube-proxy.

## Решение

Вариант 3: publish-only controller.

## Почему

- Устраняет корневую причину: kube-proxy не знал VIP.
- Нет лишнего хопа, пода в пути и `NET_ADMIN` у контроллера.
- Сетевой путь и conntrack остаются штатными.
- Keepalived продолжает владеть VIP, easyLB только публикует его.

## Последствия

- Нет hostPort/NAT-fallback для public cloud.
- Нет enforcement `loadBalancerSourceRanges`.
- Таймауты, conntrack и долгие сессии — поведение kube-proxy.

## Когда пересмотреть

Нужен public-cloud/NAT-fallback, source-range enforcement, DSR или полная
независимость от kube-proxy.
