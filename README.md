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
  <a href="https://github.com/rvbsalgado/fencemaster/stargazers"><img src="https://img.shields.io/github/stars/rvbsalgado/fencemaster?style=flat-square" alt="Stars"></a>
  <a href="https://github.com/rvbsalgado/fencemaster/network/members"><img src="https://img.shields.io/github/forks/rvbsalgado/fencemaster?style=flat-square" alt="Forks"></a>
  <a href="https://github.com/rvbsalgado/fencemaster/issues"><img src="https://img.shields.io/github/issues/rvbsalgado/fencemaster?style=flat-square" alt="Issues"></a>
  <a href="https://github.com/rvbsalgado/fencemaster/pulls"><img src="https://img.shields.io/github/issues-pr/rvbsalgado/fencemaster?style=flat-square" alt="Pull Requests"></a>
</p>

<p align="center">
  <strong>A namespace admission controller for Rancher project automation</strong>
</p>

---

## Why Fencemaster?

Managing Rancher project assignments across multiple clusters is tedious:

- **Manual assignment** - Clicking through Rancher UI for each namespace
- **Drift** - GitOps tools recreate namespaces without project assignments
- **Inconsistency** - Different clusters have different project configurations
- **No automation** - Rancher doesn't natively support declarative project assignment

**Fencemaster solves this** by letting you declare project membership as a simple label. Your GitOps pipeline (Flux, ArgoCD, etc.) creates namespaces with a `project: platform` label, and Fencemaster automatically assigns them to the correct Rancher project.

## Overview

Fencemaster enables Rancher project automation by adding project annotations to namespaces based on a simple label. It works in two ways:

- **Webhook** - A mutating admission webhook in your Rancher management cluster, called by downstream clusters
- **Controller** - A controller in each downstream cluster, for clusters whose API server can't reach the management cluster, such as EKS

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
- **Controller mode** - Works without a webhook on clusters that can't reach the management cluster
- **Hub sync** - Publishes project IDs to controller clusters with Fleet, with no Rancher credentials downstream
- **GitOps friendly** - Declarative namespace-to-project mapping
- **Namespace exclusions** - Skip system namespaces (`kube-system`, `cattle-*`, etc.)
- **Configurable** - Customize label and annotation names
- **Caching** - In-memory cache for cluster/project lookups
- **Prometheus metrics** - Monitor webhook performance and cache efficiency

## Architecture

### Webhook

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

### Controller

```
               ┌──────────────────────────────┐
               │   Rancher Management Cluster │
               │                              │
               │   fencemaster-sync           │
               │   (optional)                 │
               │                              │
               │   Watches projects, writes   │
               │   one Fleet Bundle per       │
               │   cluster                    │
               └──────────────┬───────────────┘
                              │ fleet-agent
                              ▼
               ┌──────────────────────────────┐
               │   Downstream Cluster         │
               │                              │
               │   ConfigMap of project IDs   │
               │         │ read               │
               │         ▼                    │
               │   fencemaster controller     │
               │         │ patch              │
               │         ▼                    │
               │   Namespaces                 │
               └──────────────────────────────┘
```

## How It Works

### Webhook

1. A namespace is created/updated in a downstream cluster with a `project` label
2. The MutatingWebhookConfiguration sends the admission request to Fencemaster
3. Fencemaster extracts the cluster name from the URL path (`/mutate/{cluster-name}`)
4. Looks up the cluster ID from `clusters.provisioning.cattle.io` in the management cluster
5. Looks up the project ID from `projects.management.cattle.io` using the label value
6. Returns a JSON Patch adding the `field.cattle.io/projectId` annotation
7. Rancher sees the annotation and assigns the namespace to the project

### Controller

1. The controller watches namespaces in its own cluster, and a ConfigMap of project name to `<cluster-id>:<project-id>`
2. A namespace with a `project` label is created, updated or found at startup
3. The controller looks up the label value in the ConfigMap
4. It patches the `field.cattle.io/projectId` annotation onto the namespace, just after creation
5. Rancher sees the annotation and assigns the namespace to the project

The ConfigMap can be managed by hand, by Terraform, or by the hub sync, which watches `projects.management.cattle.io` and delivers it with Fleet. A project called `Team A` becomes the key `Team-A`, so namespaces need that label value.

Controller mode overwrites an annotation that differs from the ConfigMap, including moves made in the Rancher UI, and can't reject namespaces.

## Requirements

- Rancher v2.6+ (management cluster)
- Kubernetes v1.25+

Webhook:

- Network connectivity from downstream clusters to the webhook endpoint
- TLS termination (via Istio, Gateway API, or ingress controller)

Hub sync:

- Fleet, included with Rancher

## Quick Start

```bash
# Install in management cluster
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace
```

To run the controller on a downstream cluster, and publish project IDs to it from the management cluster:

```bash
# On the downstream cluster
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n cattle-system \
  --set installMode=controller

# On the management cluster, logging what would be published
helm upgrade fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster \
  --reuse-values \
  --set sync.enabled=true \
  --set sync.dryRun=true
```

The hub sync includes all clusters by default. Label a cluster `fencemaster/project-sync=false` to skip it, or set `sync.optIn=true` to include only clusters labelled `true`.

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
| `--exclude-namespaces` | `EXCLUDE_NAMESPACES` | (see below)               | Namespaces to skip (comma-separated) |
| `--mode`               | `MODE`               | server                    | server, controller or sync         |
| `--project-ids-configmap` | `PROJECT_IDS_CONFIGMAP` | cattle-system/rancher-project-ids | Controller and sync: `<namespace>/<name>` of the project IDs ConfigMap |
| `--resync`             | `RESYNC_MINUTES`     | 10                        | Controller and sync: re-list interval in minutes |
| `--sync-cluster-selector` | `SYNC_CLUSTER_SELECTOR` | fencemaster/project-sync!=false | Sync: label selector for clusters to publish to |
| `--fleet-namespace`    | `FLEET_NAMESPACE`    | fleet-default             | Sync: namespace of clusters and Bundles |

### Namespace Exclusions

By default, the following namespaces are excluded from mutation:

- `kube-system`, `kube-public`, `kube-node-lease`, `default`
- `cattle-*` (all Rancher system namespaces)
- `fleet-*` (all Fleet namespaces)

You can customize this with the `--exclude-namespaces` flag or `EXCLUDE_NAMESPACES` environment variable. Patterns ending with `*` match prefixes:

```bash
# Custom exclusions
--exclude-namespaces="kube-system,kube-public,my-system-*"
```

## Operational Modes

### Permissive Mode (default)

Allows namespace creation even if cluster or project lookup fails. The namespace is created without the project annotation.

### Strict Mode (`--strict-mode`)

Rejects the admission request if the cluster or project cannot be found. Use this to enforce that all namespaces with the project label must be assigned to a valid project. Webhook only, as the controller acts after a namespace is created.

### Dry-Run Mode (`--dry-run`)

Logs what mutations would be applied without actually patching namespaces. Useful for testing configuration changes. On the hub sync, use `sync.dryRun=true` to log what would be published without writing Bundles.

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
