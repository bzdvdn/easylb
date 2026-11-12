# Security policy

## Supported versions

| Version | Supported |
|---|---|
| `0.1.x` | yes |
| older | no |

easyLB is pre-1.0. Security fixes are made on `main` and the latest release.

## Reporting a vulnerability

Please **do not** open a public issue for security problems.

Report privately through GitHub Security Advisories:

1. Open the repository's **Security** tab.
2. Choose **Advisories** -> **Report a vulnerability**.
3. Include:
   - affected version and Kubernetes distribution;
   - installation mode (`Helm`, static manifests);
   - VIP mode (`external` or `l2`);
   - reproduction steps and expected impact;
   - relevant logs with secrets removed.

If private advisories are unavailable, contact [@bzdvdn](https://github.com/bzdvdn)
on GitHub and ask for a private channel before sharing details.

## Scope

Security-relevant areas include:

- controller and speaker RBAC;
- speaker raw-socket/NET_RAW usage and `hostNetwork`;
- Service status and `VIPBinding` ownership;
- Gateway API annotation handling;
- Helm chart defaults and Pod Security Admission behavior;
- image and chart supply chain.

## Response

The maintainers aim to acknowledge a private report within a few days, agree on
a disclosure plan, and publish a fix and advisory once users can upgrade.
