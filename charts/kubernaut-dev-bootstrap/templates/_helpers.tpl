{{- define "kubernaut-dev-bootstrap.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "kubernaut-dev-bootstrap.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.labels" -}}
helm.sh/chart: {{ include "kubernaut-dev-bootstrap.chart" . }}
app.kubernetes.io/part-of: kubernaut-dev-bootstrap
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.selectorLabels" -}}
app.kubernetes.io/part-of: kubernaut-dev-bootstrap
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.postgresqlLabels" -}}
app.kubernetes.io/name: {{ .Values.postgresql.name }}
{{ include "kubernaut-dev-bootstrap.selectorLabels" . }}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.valkeyLabels" -}}
app.kubernetes.io/name: {{ .Values.valkey.name }}
{{ include "kubernaut-dev-bootstrap.selectorLabels" . }}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.image" -}}
{{- if .image.digest -}}
{{- printf "%s@%s" .image.repository .image.digest -}}
{{- else -}}
{{- printf "%s:%s" .image.repository .image.tag -}}
{{- end -}}
{{- end -}}

{{- define "kubernaut-dev-bootstrap.containerSecurityContext" -}}
allowPrivilegeEscalation: {{ .allowPrivilegeEscalation }}
capabilities:
{{ toYaml .capabilities | nindent 2 }}
runAsNonRoot: {{ .runAsNonRoot }}
seccompProfile:
{{ toYaml .seccompProfile | nindent 2 }}
{{- end -}}
