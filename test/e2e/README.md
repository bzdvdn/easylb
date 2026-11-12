# E2E smoke test

Validates automatic VIP allocation on a k3s cluster in k3d:

1. installs the chart with the local `easylb:e2e` image;
2. creates a `LoadBalancer` Service without a `VIPBinding`;
3. waits for `auto-<service>` to allocate an address from `VIPPool`;
4. checks that the address is published in the Service status;
5. deletes the Service and verifies that the binding is garbage-collected.

Prerequisites: `docker`, `k3d`, `kubectl`, `helm`.

```bash
docker build -t easylb:e2e .

k3d cluster create easylb-e2e --agents 1 \
  --k3s-arg "--disable=servicelb@server:*"
k3d image import -c easylb-e2e easylb:e2e
kubectl wait --for=condition=Ready nodes --all --timeout=180s

./test/e2e/run.sh

k3d cluster delete easylb-e2e
```

GitHub Actions runs the same test in `.github/workflows/e2e.yaml`.
