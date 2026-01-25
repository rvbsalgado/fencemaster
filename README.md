# Fencemaster

<p align="center">
  <img src="docs/images/logo/logo-128.png" alt="Fencemaster Logo">
</p>

<p align="center">
  <a href="https://github.com/rvbsalgado/fencemaster/releases"><img src="https://img.shields.io/github/v/release/rvbsalgado/fencemaster?include_prereleases&style=for-the-badge&label=Release" alt="Release"></a>
  <a href="https://artifacthub.io/packages/helm/fencemaster/fencemaster"><img src="https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/fencemaster&style=for-the-badge" alt="Artifact Hub"></a>
  <a href="https://github.com/rvbsalgado/fencemaster/actions"><img src="https://img.shields.io/github/actions/workflow/status/rvbsalgado/fencemaster/ci.yaml?style=for-the-badge&label=CI" alt="CI"></a>
  <a href="https://go.dev/"><img src="https://img.shields.io/github/go-mod/go-version/rvbsalgado/fencemaster?style=for-the-badge" alt="Go Version"></a>
  <a href="https://github.com/rvbsalgado/fencemaster/blob/main/LICENSE"><img src="https://img.shields.io/github/license/rvbsalgado/fencemaster?style=for-the-badge" alt="License"></a>
</p>

<p align="center">
  <strong>Kubernetes admission controller that automatically assigns namespaces to Rancher projects</strong>
</p>

---

## Overview

Fencemaster is a mutating admission webhook that runs in your Rancher management cluster. It automatically adds Rancher project annotations to namespaces based on a simple label.

```yaml
# Add this label to your namespace
labels:
  project: platform

# Fencemaster automatically adds
annotations:
  field.cattle.io/projectId: c-m-xxxxx:p-xxxxx
```

## Features

- **Automatic project assignment** - Namespaces with `project` label get Rancher project annotation
- **Multi-cluster support** - Single deployment serves all downstream clusters
- **GitOps friendly** - Declarative namespace-to-project mapping
- **Caching** - In-memory cache for cluster/project lookups
- **Prometheus metrics** - Monitor webhook performance and cache efficiency

## Quick Start

```bash
# Install in management cluster
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace
```

See [charts/fencemaster](./charts/fencemaster) for full installation instructions.

## Documentation

- [Chart README](./charts/fencemaster/README.md) - Installation and configuration

## License

Apache 2.0 - See [LICENSE](LICENSE) for details
