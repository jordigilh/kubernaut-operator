{{- define "kubernaut-development-dependencies.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubernaut-development-dependencies.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "kubernaut-development-dependencies.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "kubernaut-development-dependencies.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubernaut-development-dependencies.labels" -}}
helm.sh/chart: {{ include "kubernaut-development-dependencies.chart" . }}
app.kubernetes.io/part-of: kubernaut-development-dependencies
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kubernaut-development-dependencies.selectorLabels" -}}
app.kubernetes.io/part-of: kubernaut-development-dependencies
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kubernaut-development-dependencies.postgresqlLabels" -}}
app.kubernetes.io/name: {{ .Values.postgresql.name }}
{{ include "kubernaut-development-dependencies.selectorLabels" . }}
{{- end -}}

{{- define "kubernaut-development-dependencies.valkeyLabels" -}}
app.kubernetes.io/name: {{ .Values.valkey.name }}
{{ include "kubernaut-development-dependencies.selectorLabels" . }}
{{- end -}}

{{- define "kubernaut-development-dependencies.image" -}}
{{- if .image.digest -}}
{{- printf "%s@%s" .image.repository .image.digest -}}
{{- else -}}
{{- printf "%s:%s" .image.repository .image.tag -}}
{{- end -}}
{{- end -}}

{{- define "kubernaut-development-dependencies.containerSecurityContext" -}}
allowPrivilegeEscalation: {{ .allowPrivilegeEscalation }}
capabilities:
{{ toYaml .capabilities | nindent 2 }}
runAsNonRoot: {{ .runAsNonRoot }}
seccompProfile:
{{ toYaml .seccompProfile | nindent 2 }}
{{- end -}}
