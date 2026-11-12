# 0002 — CRD как единственный источник истины

**Статус:** Accepted

## Проблема

VIP нужно настраивать, не меняя ingress-nginx или data-plane Service, и
конфигурация должна поддерживать автоматическую аллокацию и статус.

## Ограничения

- Service часто управляется Helm или реализацией Gateway.
- Для аллокации из пула нужно состояние.
- Helm должен устанавливать и шаблонизировать конфигурацию.

## Варианты

1. Аннотации на Service (`easylb.io/vips`, `easylb.io/vip-config`).
2. ConfigMap с глобальным списком VIP.
3. CRD `VIPPool` и `VIPBinding`.

## Решение

Вариант 3: только CRD.

## Почему

- Типизация и валидация API-сервером.
- Один владелец VIP, нет precedence между аннотациями и binding.
- `VIPBinding.status.allocatedIPs` — естественное место для состояния
  аллокации.
- Namespaced `VIPBinding` хорошо ложится на RBAC и multi-tenancy.
- Helm ставит CRD в `crds/`, а binding'и — шаблонами, не трогая чужие Service.

## Последствия

- CRD нужно установить до controller/speaker.
- `helm upgrade` не обновляет CRD из `crds/`.
- При миграции с аннотаций нужно создать `VIPBinding`.

## Когда пересмотреть

Нужен режим без CRD там, где CRD установить нельзя.
