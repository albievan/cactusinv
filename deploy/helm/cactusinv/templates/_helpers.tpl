{{- define "cactusinv.labels" -}}
app.kubernetes.io/part-of: cactusinv
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: Helm
{{- end }}

{{- define "cactusinv.secret" -}}
{{- if .Values.secrets.existingSecret }}{{ .Values.secrets.existingSecret }}{{ else }}{{ .Release.Name }}-secrets{{ end }}
{{- end }}

{{- define "cactusinv.image" -}}
{{ required "image.repository is required" .Values.image.repository }}:{{ default .Chart.AppVersion .Values.image.tag }}
{{- end }}
