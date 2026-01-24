# fencemaster

Kubernetes admission controller that automatically assigns Rancher projects to namespaces

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

## Installation

### Management Cluster

```bash
helm install fencemaster ./charts/fencemaster \
  -n fencemaster --create-namespace
```

### Downstream Clusters

Deploy the webhook configuration to each downstream cluster:

```bash
helm template fencemaster ./charts/fencemaster \
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
| downstreamWebhook.clusterName | string | `""` | Name of the downstream cluster (required when deploying webhook config) |
| downstreamWebhook.excludeNamespaces | list | `["kube-system","kube-public","kube-node-lease"]` | Namespaces to exclude from mutation |
| downstreamWebhook.externalUrl | string | `""` | External URL to reach the webhook from downstream clusters (e.g., https://fencemaster.example.com) |
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
| podSecurityContext | object | `{"fsGroup":65532,"runAsGroup":65532,"runAsNonRoot":true,"runAsUser":65532,"seccompProfile":{"type":"RuntimeDefault"}}` | Pod security context |
| replicaCount | int | `2` | Number of replicas for high availability |
| resources | object | `{"limits":{"cpu":"200m","memory":"128Mi"},"requests":{"cpu":"50m","memory":"64Mi"}}` | Resource requests and limits |
| securityContext | object | `{"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"readOnlyRootFilesystem":true}` | Container security context |
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
| rvbsalgado |  |  |
