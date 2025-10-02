{{/*
Expand the name of the chart.
*/}}
{{- define "mcp-bridge.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "mcp-bridge.fullname" -}}
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
{{- define "mcp-bridge.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "mcp-bridge.labels" -}}
helm.sh/chart: {{ include "mcp-bridge.chart" . }}
{{ include "mcp-bridge.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "mcp-bridge.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mcp-bridge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Gateway labels
*/}}
{{- define "mcp-bridge.gateway.labels" -}}
helm.sh/chart: {{ include "mcp-bridge.chart" . }}
{{ include "mcp-bridge.gateway.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: gateway
{{- end }}

{{/*
Gateway selector labels
*/}}
{{- define "mcp-bridge.gateway.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mcp-bridge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: gateway
{{- end }}

{{/*
Router labels
*/}}
{{- define "mcp-bridge.router.labels" -}}
helm.sh/chart: {{ include "mcp-bridge.chart" . }}
{{ include "mcp-bridge.router.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: router
{{- end }}

{{/*
Router selector labels
*/}}
{{- define "mcp-bridge.router.selectorLabels" -}}
app.kubernetes.io/name: {{ include "mcp-bridge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: router
{{- end }}

{{/*
Create the name of the service account to use for gateway
*/}}
{{- define "mcp-bridge.gateway.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-gateway" (include "mcp-bridge.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the service account to use for router
*/}}
{{- define "mcp-bridge.router.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (printf "%s-router" (include "mcp-bridge.fullname" .)) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Generate certificates for TLS
*/}}
{{- define "mcp-bridge.gen-certs" -}}
{{- $altNames := list ( printf "%s.%s" (include "mcp-bridge.fullname" .) .Release.Namespace ) ( printf "%s.%s.svc" (include "mcp-bridge.fullname" .) .Release.Namespace ) -}}
{{- $ca := genCA "mcp-bridge-ca" 365 -}}
{{- $cert := genSignedCert ( include "mcp-bridge.fullname" . ) nil $altNames 365 $ca -}}
tls.crt: {{ $cert.Cert | b64enc }}
tls.key: {{ $cert.Key | b64enc }}
ca.crt: {{ $ca.Cert | b64enc }}
{{- end }}

{{/*
Generate JWT secret key
*/}}
{{- define "mcp-bridge.gen-jwt-secret" -}}
{{- if .Values.secrets.jwtSecretKey }}
{{- .Values.secrets.jwtSecretKey | b64enc }}
{{- else }}
{{- randAlphaNum 32 | b64enc }}
{{- end }}
{{- end }}

{{/*
Generate MCP auth token
*/}}
{{- define "mcp-bridge.gen-mcp-token" -}}
{{- if .Values.secrets.mcpAuthToken }}
{{- .Values.secrets.mcpAuthToken | b64enc }}
{{- else }}
{{- printf "mcp-auth-%s" (randAlphaNum 16) | b64enc }}
{{- end }}
{{- end }}

{{/*
Generate Redis password
*/}}
{{- define "mcp-bridge.gen-redis-password" -}}
{{- if .Values.secrets.redisPassword }}
{{- .Values.secrets.redisPassword | b64enc }}
{{- else }}
{{- randAlphaNum 24 | b64enc }}
{{- end }}
{{- end }}

{{/*
Return the proper image name for gateway
*/}}
{{- define "mcp-bridge.gateway.image" -}}
{{- with .Values.gateway.image }}
{{- if .registry }}
{{- printf "%s/%s:%s" .registry .repository (.tag | default $.Chart.AppVersion) }}
{{- else }}
{{- printf "%s:%s" .repository (.tag | default $.Chart.AppVersion) }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Return the proper image name for router
*/}}
{{- define "mcp-bridge.router.image" -}}
{{- with .Values.router.image }}
{{- if .registry }}
{{- printf "%s/%s:%s" .registry .repository (.tag | default $.Chart.AppVersion) }}
{{- else }}
{{- printf "%s:%s" .repository (.tag | default $.Chart.AppVersion) }}
{{- end }}
{{- end }}
{{- end }}