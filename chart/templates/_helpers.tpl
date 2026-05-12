{{/*
Canonical names. tenant-a-dev → tenant-a-monitoring-dev for the namespace.
*/}}
{{- define "tenant.namespace" -}}
{{- if .Values.namespace -}}
{{ .Values.namespace }}
{{- else -}}
{{ .Values.tenant.name }}-monitoring-{{ .Values.tenant.env }}
{{- end -}}
{{- end -}}
 
{{- define "router.fullname" -}}
{{ .Values.tenant.name }}-alert-router
{{- end -}}
 
{{- define "router.labels" -}}
app.kubernetes.io/name: tenant-alert-router
app.kubernetes.io/instance: {{ .Values.tenant.name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
tenant: {{ .Values.tenant.name }}
{{- end -}}
 
{{- define "telegram.secretName" -}}
{{- if .Values.telegram.existingSecret -}}
{{ .Values.telegram.existingSecret }}
{{- else -}}
{{ .Values.tenant.name }}-telegram
{{- end -}}
{{- end -}}