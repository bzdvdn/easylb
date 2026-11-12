{{- define "easylb.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "easylb.fullname" -}}
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

{{- define "easylb.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "easylb.labels" -}}
helm.sh/chart: {{ include "easylb.chart" . }}
{{ include "easylb.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "easylb.selectorLabels" -}}
app.kubernetes.io/name: {{ include "easylb.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "easylb.controllerSelectorLabels" -}}
{{ include "easylb.selectorLabels" . }}
app.kubernetes.io/component: controller
{{- end -}}

{{- define "easylb.speakerSelectorLabels" -}}
{{ include "easylb.selectorLabels" . }}
app.kubernetes.io/component: speaker
{{- end -}}

{{- define "easylb.controllerImage" -}}
{{- printf "%s:%s" .Values.controller.image.repository (default .Chart.AppVersion .Values.controller.image.tag) -}}
{{- end -}}

{{- define "easylb.speakerImage" -}}
{{- printf "%s:%s" .Values.speaker.image.repository (default .Chart.AppVersion .Values.speaker.image.tag) -}}
{{- end -}}
