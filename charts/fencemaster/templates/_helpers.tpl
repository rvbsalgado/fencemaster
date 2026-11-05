{{/*
Expand the name of the chart.
*/}}
{{- define "fencemaster.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "fencemaster.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "fencemaster.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "fencemaster.labels" -}}
helm.sh/chart: {{ include "fencemaster.chart" . }}
{{ include "fencemaster.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "fencemaster.selectorLabels" -}}
app.kubernetes.io/name: {{ include "fencemaster.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "fencemaster.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "fencemaster.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Image name
*/}}
{{- define "fencemaster.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Hub sync resources. The name label differs from the server's so that the Service
and PodDisruptionBudget, which select on it, never match sync pods.
*/}}
{{- define "fencemaster.sync.fullname" -}}
{{- printf "%s-sync" (include "fencemaster.fullname" . | trunc 58 | trimSuffix "-") }}
{{- end }}

{{- define "fencemaster.sync.selectorLabels" -}}
app.kubernetes.io/name: {{ printf "%s-sync" (include "fencemaster.name" . | trunc 58 | trimSuffix "-") }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{- define "fencemaster.sync.labels" -}}
helm.sh/chart: {{ include "fencemaster.chart" . }}
{{ include "fencemaster.sync.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end }}

{{- define "fencemaster.sync.clusterSelector" -}}
{{- if .Values.sync.clusterSelector }}
{{- .Values.sync.clusterSelector }}
{{- else if .Values.sync.optIn }}
{{- "fencemaster/project-sync=true" }}
{{- else }}
{{- "fencemaster/project-sync!=false" }}
{{- end }}
{{- end }}

{{- define "fencemaster.sync.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- include "fencemaster.sync.fullname" . }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
