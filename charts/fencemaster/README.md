# fencemaster

Kubernetes admission controller that automatically assigns namespaces to Rancher projects

![Version: 0.1.17](https://img.shields.io/badge/Version-0.1.17-informational?style=flat-square)  ![AppVersion: 1.0.0-rc.24](https://img.shields.io/badge/AppVersion-1.0.0--rc.24-informational?style=flat-square)

[![Artifact Hub](https://img.shields.io/endpoint?url=https://artifacthub.io/badge/repository/fencemaster&style=flat-square)](https://artifacthub.io/packages/helm/fencemaster/fencemaster)
![License](https://img.shields.io/github/license/rvbsalgado/fencemaster?style=flat-square)

## Installation

### Add the OCI Repository

```bash
# Helm 3.8+ supports OCI registries natively, no `helm repo add` needed
```

### Management Cluster

Install fencemaster in your Rancher management cluster:

```bash
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace
```

Or with custom values:

```bash
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace \
  --set webhook.strictMode=true \
  --set logging.level=debug
```

### Downstream Clusters (Webhook Only)

Deploy the webhook configuration to each downstream cluster using **helm install**:

```bash
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace \
  --set installMode=webhook \
  --set downstreamWebhook.externalUrl=https://fencemaster.management.example.com \
  --set downstreamWebhook.clusterName=my-cluster
```

### Downstream Clusters (Controller, No Webhook)

Use `installMode=controller` when the downstream API server can't reach the management
cluster's webhook URL (e.g. EKS, whose control plane egress isn't from your NAT IPs).
fencemaster then runs in the downstream cluster, watches namespaces, and sets the project
annotation itself shortly after create/update. Project IDs come from a local ConfigMap
mapping project name to `<cluster-id>:<project-id>`, so no Rancher access is needed:

```bash
kubectl -n cattle-system create configmap rancher-project-ids \
  --from-literal=platform=c-abc12:p-xyz34

helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n cattle-system \
  --set installMode=controller \
  --set controller.projectIdsConfigMap=cattle-system/rancher-project-ids
```

Namespaces that exist when it starts are also reconciled, and a project added to the
ConfigMap later is applied to namespaces already carrying its label.

### Keeping Project IDs Up to Date (Hub Sync)

Instead of managing the ConfigMap yourself, enable the hub sync on your management
cluster install. It watches Rancher projects and writes one Fleet Bundle per cluster, and
`fleet-agent` applies the ConfigMap downstream, so downstream clusters need no Rancher
credentials:

```bash
helm upgrade --install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n cattle-system \
  --reuse-values \
  --set sync.enabled=true
```

All provisioning clusters are included by default. To skip one, label it:

```bash
kubectl -n fleet-default label clusters.provisioning.cattle.io my-cluster \
  fencemaster/project-sync=false
```

To include only the clusters you choose, set `sync.optIn=true` and label those clusters
`fencemaster/project-sync=true`. For anything else, set `sync.clusterSelector`.

Use `sync.dryRun=true` first to log what would be published. It runs as a separate
Deployment, so a problem with it can't affect the webhook, and it has its own
ServiceAccount, so webhook pods can't write Bundles.

ConfigMap keys are project display names with characters other than letters, digits, `-`,
`_` and `.` replaced by `-`, so `Team A` needs the namespace label `project: Team-A`.
Projects that end up with the same key are skipped and logged as errors.

A cluster that stops matching keeps its Bundle and ConfigMap, but they stop updating. Deleting
the Bundle deletes the ConfigMap, after which new namespaces stay unassigned.

### Combined Installation (Management + Webhook)

Install fencemaster in "all" mode to run both server and webhook in a single cluster:

```bash
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace \
  --set installMode=all
```

When using `installMode=all` with a downstream cluster, the webhook automatically uses the internal service  and `local` as cluster name (no `externalUrl` or `clusterName` needed):

## Usage

### Basic Project Assignment

Add a `project` label to your namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-app
  labels:
    project: platform
```

The webhook automatically adds the Rancher project annotation:

```yaml
metadata:
  annotations:
    field.cattle.io/projectId: c-xxxxx:p-xxxxx
```

### Advanced Configuration Examples

#### 1. Enable Strict Mode (Reject unlabeled namespaces)

```bash
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace \
  --set webhook.strictMode=true
```

This will prevent creation of namespaces without a valid project label.

#### 2. Dry-Run Mode (Preview mutations without applying)

```bash
helm install fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  -n fencemaster --create-namespace \
  --set webhook.dryRun=true \
  --set logging.level=debug
```

Useful for testing namespace mutation logic.

View logs:

```bash
kubectl logs -n fencemaster deployment/fencemaster -f
```

### Capabilities Summary

| Feature                   | Use Case                                                     |
|---------------------------|--------------------------------------------------------------|
| **Strict Mode**           | Enforce project assignment on all namespaces                 |
| **Dry-Run Mode**          | Test mutations before production deployment                  |
| **Caching**               | Reduce API calls to Rancher (configurable TTL)               |
| **Multi-Cluster**         | Support for management + multiple downstream clusters        |
| **High Availability**     | 3+ replicas with topology spread constraints                 |
| **Observability**         | Prometheus metrics and structured logging                    |
| **Security**              | Non-root user, read-only filesystem, no privilege escalation |
| **Gateway API**           | Modern ingress alternative to traditional Ingress            |
| **Pod Disruption Budget** | Maintain availability during cluster maintenance             |
| **Failure Policy**        | Choose to fail/ignore if webhook is unreachable              |

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for pod scheduling |
| commonLabels | object | `{}` | Common labels to apply to all resources |
| controller.projectIdsConfigMap | string | `"cattle-system/rancher-project-ids"` | `<namespace>/<name>` of a ConfigMap mapping project name to its annotation value ("<cluster-id>:<project-id>") |
| controller.resyncMinutes | int | `10` | Full re-list interval in minutes (also retries failed patches) |
| downstreamWebhook.clusterName | string | `""` | Name of the downstream cluster (defaults to "local" when installMode=all) |
| downstreamWebhook.excludeNamespaces | list | `["kube-system","kube-public","kube-node-lease"]` | Namespaces to exclude from mutation |
| downstreamWebhook.externalUrl | string | `""` | External URL to reach the webhook from downstream clusters (e.g., https://fencemaster.example.com). When installMode=all and this is empty, uses internal service reference. |
| downstreamWebhook.failurePolicy | string | `"Fail"` | Webhook failure policy (Fail or Ignore) |
| fullnameOverride | string | `""` | Override the full name of the release |
| gateway.annotations | object | `{}` | Additional HTTPRoute annotations |
| gateway.enabled | bool | `false` | Enable Gateway API HTTPRoute |
| gateway.hostnames | list | `[]` | Hostnames for the HTTPRoute |
| gateway.parentRefs | list | `[]` | Gateway references for HTTPRoute |
| image.pullPolicy | string | `"IfNotPresent"` | Image pull policy |
| image.repository | string | `"ghcr.io/rvbsalgado/fencemaster"` | Container image repository |
| image.tag | string | `""` | Image tag (defaults to chart appVersion) |
| imagePullSecrets | list | `[]` | Image pull secrets for private registries |
| installMode | string | `"server"` | Installation mode: "server" (management cluster), "webhook" (downstream cluster), "all" (both), or "controller" (downstream cluster, watches namespaces locally - no webhook, works where the API server can't reach the management cluster, e.g. EKS) |
| logging.format | string | `"json"` | Log format (json, text) |
| logging.level | string | `"info"` | Log level (debug, info, warn, error) |
| metrics.port | int | `9090` | Port for Prometheus metrics endpoint |
| metrics.serviceMonitor.enabled | bool | `false` | Create a ServiceMonitor for Prometheus Operator |
| metrics.serviceMonitor.interval | string | `"30s"` | Scrape interval |
| metrics.serviceMonitor.labels | object | `{}` | Additional labels for ServiceMonitor |
| metrics.serviceMonitor.scrapeTimeout | string | `"10s"` | Scrape timeout |
| nameOverride | string | `""` | Override the name of the chart |
| nodeSelector | object | `{}` | Node selector for pod scheduling |
| podAnnotations | object | `{}` | Annotations to add to pods |
| podDisruptionBudget.enabled | bool | `true` | Enable pod disruption budget |
| podDisruptionBudget.minAvailable | int | `1` | Minimum available pods during disruption |
| podLabels | object | `{}` | Labels to add to pods |
| podSecurityContext | object | `{"fsGroup":65532,"runAsGroup":65532,"runAsNonRoot":true,"runAsUser":65532,"seccompProfile":{"type":"RuntimeDefault"}}` | Pod security context |
| replicaCount | int | `2` | Number of replicas for high availability |
| resources | object | `{"limits":{"cpu":"200m","memory":"128Mi"},"requests":{"cpu":"50m","memory":"64Mi"}}` | Resource requests and limits |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | Container security context |
| service.annotations | object | `{}` | Additional annotations to add to the service |
| service.labels | object | `{}` | Additional labels to add to the service |
| service.port | int | `80` | Service port |
| service.type | string | `"ClusterIP"` | Service type |
| serviceAccount.annotations | object | `{}` | Annotations to add to the service account |
| serviceAccount.create | bool | `true` | Create a service account |
| serviceAccount.name | string | `""` | Name of the service account (auto-generated if empty) |
| sync.clusterSelector | string | `""` | Custom label selector for the provisioning clusters to publish to, used instead of `optIn` when set |
| sync.dryRun | bool | `false` | Log what would be published without writing Fleet Bundles |
| sync.enabled | bool | `false` | Deploy the hub sync (only with installMode "server" or "all") |
| sync.fleetNamespace | string | `"fleet-default"` | Namespace of the provisioning clusters and their Fleet Bundles |
| sync.optIn | bool | `false` | Only publish to clusters labelled `fencemaster/project-sync=true`. By default all clusters are included, except those labelled `fencemaster/project-sync=false` |
| sync.projectIdsConfigMap | string | `"cattle-system/rancher-project-ids"` | `<namespace>/<name>` of the ConfigMap to create on downstream clusters. Must match `controller.projectIdsConfigMap` there |
| sync.resyncMinutes | int | `10` | Full re-list interval in minutes |
| tolerations | list | `[]` | Tolerations for pod scheduling |
| topologySpreadConstraints.enabled | bool | `true` | Enable topology spread constraints for HA |
| topologySpreadConstraints.maxSkew | int | `1` | Maximum allowed skew between zones/nodes |
| topologySpreadConstraints.whenUnsatisfiable | string | `"ScheduleAnyway"` | How to handle unsatisfiable constraints (ScheduleAnyway, DoNotSchedule) |
| webhook.cacheTTLMinutes | int | `5` | Cache TTL in minutes for cluster/project lookups |
| webhook.dryRun | bool | `false` | Log what would happen without actually patching namespaces |
| webhook.excludeNamespaces | list | `["kube-system", "kube-public", "kube-node-lease", "default", "cattle-*", "fleet-*"]` | Namespaces to exclude from mutation (supports * suffix for prefix matching) |
| webhook.port | int | `8080` | Port the webhook server listens on |
| webhook.projectAnnotation | string | `"field.cattle.io/projectId"` | Annotation key to set on namespace for Rancher project assignment |
| webhook.projectLabel | string | `"project"` | Namespace label to read project name from |
| webhook.strictMode | bool | `false` | Reject namespace if project not found (default: allow without annotation) |

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| rvbsalgado | <rvbsalgado@users.noreply.github.com> | <https://github.com/rvbsalgado> |
