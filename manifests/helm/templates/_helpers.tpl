{{- define "slimserve.namespace" -}}
{{- ternary "slimserve" .Release.Namespace (eq .Release.Namespace "default") -}}
{{- end -}}

{{- define "slimserve.main" -}}
{{- printf "%s-main" .Release.Name -}}
{{- end -}}

{{- define "slimserve.claim" -}}
{{- printf "%s-data-pvc" .Release.Name -}}
{{- end -}}

{{- define "slimserve.secret" -}}
{{- printf "%s-runtime" .Release.Name -}}
{{- end -}}
