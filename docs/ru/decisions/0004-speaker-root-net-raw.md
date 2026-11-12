# 0004 — Speaker запускается как root с NET_RAW

**Статус:** Accepted

## Проблема

Speaker открывает `AF_PACKET` сокет для ARP. Non-root контейнер с `NET_RAW`
может получить `operation not permitted`, потому что default runtime не даёт
эффективный capability.

## Ограничения

- Нужны raw ARP-сокеты и `hostNetwork`.
- Нельзя получать другие capabilities.
- Controller должен оставаться non-root.

## Варианты

1. Non-root + `NET_RAW`.
2. Non-root + file capabilities (`cap_net_raw+ep`) на бинаре.
3. Root + `NET_RAW`, все остальные capabilities сброшены.

## Решение

Вариант 3 для speaker. Controller остаётся non-root.

## Почему

- Стабильно работает на разных container runtime.
- Speaker — привилегированный сетевой компонент, как keepalived или speaker
  MetalLB.
- Остальные capabilities сброшены; контейнер не privileged.

## Последствия

- Pod Security Admission должен разрешать speaker (например,
  `pod-security.kubernetes.io/enforce=privileged`).
- Speaker — более ценная цель; образ должен быть минимальным, RBAC узким.

## Когда пересмотреть

File capabilities станут надёжными в целевом runtime или PSA запретит root.
