# fencemaster

Kubernetes admission controller that automatically assigns namespaces to Rancher projects

![Version: 0.1.7](https://img.shields.io/badge/Version-0.1.7-informational?style=flat-square)  ![AppVersion: 1.0.0-rc.6](https://img.shields.io/badge/AppVersion-1.0.0--rc.6-informational?style=flat-square) 

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

### Downstream Clusters

Deploy the webhook configuration to each downstream cluster:

```bash
helm template fencemaster oci://ghcr.io/rvbsalgado/charts/fencemaster \
  --set downstreamWebhook.externalUrl=https://webhook.example.com \
  --set downstreamWebhook.clusterName=my-cluster \
  -s templates/mutatingwebhook.yaml | kubectl apply -f -
```

## Usage

Add a `project` label to your namespace:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: my-app
  labels:
    project: platform
```

The webhook automatically adds the Rancher project annotation.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` | Affinity rules for pod scheduling |
| commonLabels | object | `{}` | Common labels to apply to all resources |
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
| installMode | string | `"server"` | Installation mode: "server" (management cluster), "webhook" (downstream cluster), or "all" (both) |
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
| tolerations | list | `[]` | Tolerations for pod scheduling |
| topologySpreadConstraints.enabled | bool | `true` | Enable topology spread constraints for HA |
| topologySpreadConstraints.maxSkew | int | `1` | Maximum allowed skew between zones/nodes |
| topologySpreadConstraints.whenUnsatisfiable | string | `"ScheduleAnyway"` | How to handle unsatisfiable constraints (ScheduleAnyway, DoNotSchedule) |
| webhook.cacheTTLMinutes | int | `5` | Cache TTL in minutes for cluster/project lookups |
| webhook.dryRun | bool | `false` | Log what would happen without actually patching namespaces |
| webhook.port | int | `8080` | Port the webhook server listens on |
| webhook.strictMode | bool | `false` | Reject namespace if project not found (default: allow without annotation) |

## Maintainers

| Name | Email | Url |
| ---- | ------ | --- |
| rvbsalgado | <rvbsalgado@users.noreply.github.com> | <https://github.com/rvbsalgado> |
