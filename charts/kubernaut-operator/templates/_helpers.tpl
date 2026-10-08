{{/*
Copyright 2026 Jordi Gil.
Licensed under the Apache License, Version 2.0.
*/}}
{{- define "kubernaut-operator.name" -}}
kubernaut-operator
{{- end }}

{{- define "kubernaut-operator.fullname" -}}
kubernaut-operator
{{- end }}

{{- define "kubernaut-operator.namespace" -}}
{{- .Release.Namespace -}}
{{- end }}

{{- define "kubernaut-operator.labels" -}}
app.kubernetes.io/name: kubernaut-operator
app.kubernetes.io/managed-by: Helm
app.kubernetes.io/part-of: kubernaut
app.kubernetes.io/instance: {{ .Release.Name | quote }}
app.kubernetes.io/component: controller-manager
{{- end }}

{{- define "kubernaut-operator.selectorLabels" -}}
app.kubernetes.io/name: kubernaut-operator
app.kubernetes.io/component: controller-manager
{{- end }}

{{- define "kubernaut-operator.serviceAccountName" -}}
{{- default (include "kubernaut-operator.fullname" .) .Values.serviceAccount.name -}}
{{- end }}

{{- define "kubernaut-operator.webhookServiceName" -}}
{{- .Values.webhook.service.name -}}
{{- end }}

{{- define "kubernaut-operator.webhookConfigurationName" -}}
kubernaut-operator-singleton
{{- end }}

{{- define "kubernaut-operator.managerDeploymentName" -}}
kubernaut-operator-controller-manager
{{- end }}

{{- define "kubernaut-operator.webhookSecretName" -}}
{{- if eq .Values.webhook.tls.mode "manual" -}}
{{- required "webhook.tls.existingSecret is required when webhook.tls.mode=manual" .Values.webhook.tls.existingSecret -}}
{{- else -}}
{{- .Values.webhook.tls.secretName -}}
{{- end -}}
{{- end }}

{{- define "kubernaut-operator.managerImage" -}}
{{- if .Values.image.digest -}}
{{ .Values.image.repository }}@{{ .Values.image.digest }}
{{- else -}}
{{ .Values.image.repository }}:{{ .Values.image.tag }}
{{- end -}}
{{- end }}

{{- define "kubernaut-operator.certBootstrapImage" -}}
{{- $image := .Values.webhook.tls.development.image -}}
{{- if $image.digest -}}
{{ $image.repository }}@{{ $image.digest }}
{{- else -}}
{{ $image.repository }}:{{ $image.tag }}
{{- end -}}
{{- end }}

{{- define "kubernaut-operator.imagePullSecrets" -}}
{{- $pullSecrets := .Values.image.pullSecrets | default (list) -}}
{{- if and .Values.webhook.enabled (eq .Values.webhook.tls.mode "development") -}}
{{- $pullSecrets = concat $pullSecrets (.Values.webhook.tls.development.image.pullSecrets | default (list)) -}}
{{- end -}}
{{- with $pullSecrets }}
imagePullSecrets:
{{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}

{{- define "kubernaut-operator.developmentImagePullSecrets" -}}
{{- with .Values.webhook.tls.development.image.pullSecrets }}
imagePullSecrets:
{{- toYaml . | nindent 2 }}
{{- end }}
{{- end }}
