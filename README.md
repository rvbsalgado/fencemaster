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
- **Configurable** - Customize label and annotation names
- **Caching** - In-memory cache for cluster/project lookups
- **Prometheus metrics** - Monitor webhook performance and cache efficiency

## Architecture

```
┌─────────────────────────────┐      ┌─────────────────────────────┐
│   Downstream Cluster A      │      │   Downstream Cluster B      │
│                             │      │                             │
│  MutatingWebhookConfig      │      │  MutatingWebhookConfig      │
│  url: .../mutate/cluster-a  │      │  url: .../mutate/cluster-b  │
└──────────────┬──────────────┘      └──────────────┬──────────────┘
               │                                    │
               └──────────────┬─────────────────────┘
                              ▼
               ┌──────────────────────────────┐
               │   Rancher Management Cluster │
               │                              │
               │   fencemaster                │
               │   /mutate/{cluster-name}     │
               │                              │
               │   Queries:                   │
               │   - clusters.provisioning... │
               │   - projects.management...   │
               └──────────────────────────────┘
```

## How It Works

1. A namespace is created/updated in a downstream cluster with a `project` label
2. The MutatingWebhookConfiguration sends the admission request to Fencemaster
3. Fencemaster extracts the cluster name from the URL path (`/mutate/{cluster-name}`)
4. Looks up the cluster ID from `clusters.provisioning.cattle.io` in the management cluster
5. Looks up the project ID from `projects.management.cattle.io` using the label value
6. Returns a JSON Patch adding the `field.cattle.io/projectId` annotation
7. Rancher sees the annotation and assigns the namespace to the project

## Requirements

- Rancher v2.6+ (management cluster)
- Kubernetes v1.25+
- Network connectivity from downstream clusters to the webhook endpoint
- TLS termination (via Istio, Gateway API, or ingress controller)

## Quick Start

```bash
# Install in management cluster
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace
```

See [charts/fencemaster](./charts/fencemaster) for full installation instructions.

## Configuration

| Flag                   | Env Var              | Default                   | Description                        |
| ---------------------- | -------------------- | ------------------------- | ---------------------------------- |
| `--port`               | `PORT`               | 8080                      | Webhook server port                |
| `--metrics-port`       | `METRICS_PORT`       | 9090                      | Metrics server port                |
| `--log-level`          | `LOG_LEVEL`          | info                      | Log level (debug, info, warn, error) |
| `--log-format`         | `LOG_FORMAT`         | json                      | Log format (json, text)            |
| `--strict-mode`        | `STRICT_MODE`        | false                     | Reject on lookup failures          |
| `--dry-run`            | `DRY_RUN`            | false                     | Log mutations without applying     |
| `--cache-ttl`          | `CACHE_TTL_MINUTES`  | 5                         | Cache entry TTL in minutes         |
| `--project-label`      | `PROJECT_LABEL`      | project                   | Namespace label to read            |
| `--project-annotation` | `PROJECT_ANNOTATION` | field.cattle.io/projectId | Annotation key to set              |

## Operational Modes

### Permissive Mode (default)

Allows namespace creation even if cluster or project lookup fails. The namespace is created without the project annotation.

### Strict Mode (`--strict-mode`)

Rejects the admission request if the cluster or project cannot be found. Use this to enforce that all namespaces with the project label must be assigned to a valid project.

### Dry-Run Mode (`--dry-run`)

Logs what mutations would be applied without actually patching namespaces. Useful for testing configuration changes.

## Metrics

Fencemaster exposes Prometheus metrics on port 9090 (configurable):

| Metric | Type | Description |
| ------ | ---- | ----------- |
| `fencemaster_requests_total` | Counter | Total webhook requests by operation and status |
| `fencemaster_request_duration_seconds` | Histogram | Request processing duration |
| `fencemaster_cache_hits_total` | Counter | Cache hits by type (cluster, project) |
| `fencemaster_cache_misses_total` | Counter | Cache misses by type |
| `fencemaster_cluster_lookup_errors_total` | Counter | Cluster lookup errors by error type |
| `fencemaster_project_lookup_errors_total` | Counter | Project lookup errors by error type |

## Contributing

```bash
# Build
make build

# Run tests
make test

# Run linter
make lint

# Run all checks
make check

# Build Docker image
make docker-build
```

## Documentation

- [Chart README](./charts/fencemaster/README.md) - Installation and configuration

## License

Apache 2.0 - See [LICENSE](LICENSE) for details
