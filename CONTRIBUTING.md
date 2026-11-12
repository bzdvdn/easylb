# Contributing to easyLB

Thanks for helping improve easyLB. The project is small, so focused issues,
reproducible bug reports, and small pull requests are especially valuable.

## Ways to contribute

- report a bug with the bug report template;
- request a feature or describe a use case;
- improve documentation (English and Russian are both welcome);
- add or improve tests and examples;
- review pull requests.

## Development setup

Prerequisites:

- Go 1.26+;
- Docker (image and e2e builds);
- Helm 3+;
- kubectl;
- k3d for the local e2e smoke test.

```bash
make build
make test
make vet
make lint
make sec
make helm-lint
make helm-template
```

Run the local e2e test:

```bash
docker build -t easylb:e2e .
k3d cluster create easylb-e2e --agents 1 \
  --k3s-arg "--disable=servicelb@server:*"
k3d image import -c easylb-e2e easylb:e2e
kubectl wait --for=condition=Ready nodes --all --timeout=180s
make e2e
k3d cluster delete easylb-e2e
```

See `test/e2e/README.md` for details.

## Code guidelines

- keep the controller control-plane only: do not add a packet dataplane;
- the source of truth for VIPs is `VIPPool`/`VIPBinding`;
- preserve the “one VIP, one owner” rule unless `sharingKey` is used;
- do not make the speaker depend on a specific cloud or CNI;
- add tests for every behavior change;
- run `gofmt`, `go vet`, `golangci-lint`, and `gosec` before opening a PR;
- update both `docs/en` and `docs/ru` when user-facing behavior changes.

## Commit and PR style

Use short imperative commit subjects, preferably Conventional Commits:

```text
feat: add IPv6 NDP responder
fix: avoid controller name collision
docs: add ServiceLB migration guide
```

A pull request should include:

- what changed and why;
- how it was tested (commands);
- documentation updates;
- any known limitations or follow-ups.

## Licensing

By contributing, you agree that your contributions are licensed under the
Apache License 2.0. See [LICENSE](LICENSE).

## Code of conduct

All participation is covered by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).

## Security

Do not open a public issue for security problems. Follow
[SECURITY.md](SECURITY.md).
