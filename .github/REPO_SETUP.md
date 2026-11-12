# Repository setup checklist

Use these GitHub settings to make the project discoverable and welcoming.

## About

Description:

> Lightweight Kubernetes LoadBalancer controller and ServiceLB / MetalLB L2
> alternative for bare-metal, on-prem, k3s and RKE2 clusters.

Website:

> https://bzdvdn.github.io/easylb/

Suggested topics:

`kubernetes`, `loadbalancer`, `metallb`, `servicelb`, `klipper`, `k3s`,
`rke2`, `bare-metal`, `on-prem`, `gateway-api`, `arp`, `ndp`, `high-availability`

## GitHub features

- [ ] Enable **Discussions** (questions, ideas, show-and-tell).
- [ ] Enable **Private vulnerability reporting** and point to `SECURITY.md`.
- [ ] Set **Social preview** image (use the banner from `docs/assets/`).
- [ ] Enable **Dependabot alerts** and security updates.
- [ ] Enable **CodeQL** (workflow included in `.github/workflows/codeql.yaml`).
- [ ] Enable **GitHub Pages** with source **GitHub Actions**.
- [ ] Make GHCR packages public (`easylb` and `charts/easylb`).
- [ ] Protect the default branch (`master` today): require CI, no force pushes,
  linear history.
- [ ] Create the first release `v0.1.0` with the chart `.tgz` attached.
- [ ] Pin the repository in the GitHub profile and add the docs link.

## Community files

- `LICENSE` (Apache-2.0)
- `CONTRIBUTING.md`
- `CODE_OF_CONDUCT.md`
- `SECURITY.md`
- `CHANGELOG.md`
- `ADOPTERS.md`
- issue and pull request templates
- `ROADMAP.md`
