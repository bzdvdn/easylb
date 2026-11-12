#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NAMESPACE="${E2E_NAMESPACE:-default}"
SERVICE="${E2E_SERVICE:-e2e}"

kubectl get nodes >/dev/null

helm upgrade --install easylb "${ROOT}/charts/easylb" \
  --namespace easylb-system \
  --create-namespace \
  -f "${ROOT}/test/e2e/values.yaml" \
  --wait --timeout 3m

kubectl -n "${NAMESPACE}" create service loadbalancer "${SERVICE}" --tcp=80:80 --dry-run=client -o yaml | kubectl apply -f -

echo "waiting for auto VIPBinding"
for _ in $(seq 1 60); do
  if kubectl -n "${NAMESPACE}" get vipbinding "auto-${SERVICE}" >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

kubectl -n "${NAMESPACE}" get vipbinding "auto-${SERVICE}" -o yaml

VIP=""
for _ in $(seq 1 60); do
  VIP="$(kubectl -n "${NAMESPACE}" get vipbinding "auto-${SERVICE}" -o jsonpath='{.status.allocatedIPs[0]}' 2>/dev/null || true)"
  if [ -n "${VIP}" ]; then
    break
  fi
  sleep 2
done
if [ -z "${VIP}" ]; then
  echo "VIPBinding did not allocate an address" >&2
  kubectl -n "${NAMESPACE}" get vipbinding "auto-${SERVICE}" -o yaml >&2 || true
  exit 1
fi

echo "allocated VIP: ${VIP}"

STATUS_IP=""
for _ in $(seq 1 60); do
  STATUS_IP="$(kubectl -n "${NAMESPACE}" get svc "${SERVICE}" -o jsonpath='{.status.loadBalancer.ingress[0].ip}' 2>/dev/null || true)"
  if [ "${STATUS_IP}" = "${VIP}" ]; then
    break
  fi
  sleep 2
done
if [ "${STATUS_IP}" != "${VIP}" ]; then
  echo "Service status has ${STATUS_IP:-<empty>}, want ${VIP}" >&2
  kubectl -n "${NAMESPACE}" get svc "${SERVICE}" -o yaml >&2
  exit 1
fi

echo "deleting Service and waiting for VIP release"
kubectl -n "${NAMESPACE}" delete service "${SERVICE}" --wait=true
for _ in $(seq 1 60); do
  if ! kubectl -n "${NAMESPACE}" get vipbinding "auto-${SERVICE}" >/dev/null 2>&1; then
    echo "e2e passed: ${VIP}"
    exit 0
  fi
  sleep 2
done

echo "auto VIPBinding was not garbage-collected" >&2
kubectl -n "${NAMESPACE}" get vipbinding "auto-${SERVICE}" -o yaml >&2 || true
exit 1
