{{- define "holt.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "holt.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{- define "holt.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "holt.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "holt.selectorLabels" -}}
app.kubernetes.io/name: {{ include "holt.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "holt.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "holt.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
The tunnel URL stamped into join tokens: the explicit hub.advertiseAddr
wins; otherwise an enabled tunnel ingress IS the address peers dial, so
advertise wss://<its host><its non-root path> (TLS under the WebSocket,
terminated at the edge). Empty when neither is set (the hub falls back
to its bind address).
*/}}
{{- define "holt.advertiseAddr" -}}
{{- if .Values.hub.advertiseAddr -}}
{{- .Values.hub.advertiseAddr -}}
{{- else if .Values.ingress.tunnel.enabled -}}
{{- $host := required "ingress.tunnel.host is required when ingress.tunnel.enabled" .Values.ingress.tunnel.host -}}
{{- $path := .Values.ingress.tunnel.path | default "/" -}}
{{- printf "wss://%s%s" $host (ternary "" (trimSuffix "/" $path) (eq $path "/")) -}}
{{- end -}}
{{- end -}}

{{/*
Whether a shared PostgreSQL presence directory is configured, from any
of the three sources. Renders "true" or "" so it works in `if`.
*/}}
{{- define "holt.postgresEnabled" -}}
{{- if or .Values.postgres.dsn .Values.postgres.existingSecret.name .Values.postgres.cnpg.enabled -}}true{{- end -}}
{{- end -}}

{{/*
Guard: the three DSN sources are mutually exclusive; fail the render
early with a readable message instead of silently preferring one.
*/}}
{{- define "holt.postgresValidate" -}}
{{- $n := 0 -}}
{{- if .Values.postgres.dsn }}{{ $n = add1 $n }}{{ end -}}
{{- if .Values.postgres.existingSecret.name }}{{ $n = add1 $n }}{{ end -}}
{{- if .Values.postgres.cnpg.enabled }}{{ $n = add1 $n }}{{ end -}}
{{- if gt $n 1 -}}
{{- fail "postgres: set only one of postgres.dsn, postgres.existingSecret.name, postgres.cnpg.enabled" -}}
{{- end -}}
{{- end -}}
