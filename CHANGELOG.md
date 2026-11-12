# Changelog

All notable changes to easyLB are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
semantic versioning once it reaches 1.0.

## [Unreleased]

## [0.1.2] - 2026-11-12

### Changed

- Update Kubernetes patch dependencies to `0.34.13`.
- Update Go minor/patch dependencies, including `prometheus/client_golang`
  and `golang.org/x/net`.
- Update GitHub Actions to current major versions and validate the release
  workflow with them.

## [0.1.1] - 2026-11-12

### Added

- Apache-2.0 license.
- `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, `SECURITY.md`, `ADOPTERS.md`, issue
  and pull request templates, and a CodeQL workflow.
- MetalLB and ServiceLB migration guides.
- Ready-to-use examples for ingress-nginx, Envoy Gateway, and NGINX Gateway
  Fabric.
- Community page, repository setup checklist, README banner, badges, and a
  short ServiceLB/MetalLB comparison table.
- Chart metadata for Artifact Hub (`home`, `icon`, `sources`, `maintainers`,
  `keywords`).

### Changed

- Group Dependabot updates to reduce pull request noise.
- Trigger CI, Docs, E2E, and CodeQL on both `main` and `master`; docs links and
  edit URLs point to `master`.

## [0.1.0] - 2026-11-12

Initial public release.

### Added

- easyLB controller publishing node IPs and VIPs into
  `Service.status.loadBalancer.ingress`.
- `VIPPool` and `VIPBinding` CRDs with explicit VIPs, pool allocation, and
  `requestedIP`.
- `external` mode for keepalived/VRRP and other externally managed VIPs.
- `l2` speaker with ARP, IPv6 NDP, per-VIP Leases, leader election, and
  failover.
- Automatic `VIPBinding` creation from matching `VIPPool` objects, with
  namespace/service selectors, `avoidBuggyIPs`, and exhausted-pool fallback.
- MetalLB L2 compatibility: `loadBalancerIP`, `loadBalancerIPs`,
  `address-pool`, `allow-shared-ip`, and `sharingKey`.
- Gateway API `v1` integration: `Gateway` -> `VIPBinding`.
- Prometheus metrics and an optional ServiceMonitor.
- Helm chart, OCI chart publishing, and multi-arch images.
- EN/RU documentation and architecture decision records.
- CI for tests, lint, gosec, Helm, image build, docs, release, and a k3d e2e
  smoke test.

### Fixed

- Unique controller names for the service and service-allocation reconcilers.
- Shared VIP conflict handling with non-overlapping port checks.
- IPv6 DAD handling in the NDP responder.
