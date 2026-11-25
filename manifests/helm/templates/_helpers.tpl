{{- define "slimserve.namespace" -}}
{{- ternary "slimserve" .Release.Namespace (eq .Release.Namespace "default") -}}
{{- end -}}

{{- define "slimserve.main" -}}
{{- printf "%s-main" .Release.Name -}}
{{- end -}}

{{- define "slimserve.claim" -}}
{{- printf "%s-data-pvc" .Release.Name -}}
{{- end -}}

{{- define "slimserve.nodes" -}}
{{- printf "%s-nodes" .Release.Name -}}
{{- end -}}

{{- define "slimserve.secret" -}}
{{- printf "%s-runtime" .Release.Name -}}
{{- end -}}

{{- define "slimserve.credentials" -}}
{{- $auth := .Values.auth -}}
{{- $credentials := dict -}}
{{- range $key, $value := dict "SLIMSERVE_PASSWORD" $auth.password "SLIMSERVE_WRITE_PASSWORD" $auth.writePassword "SECRET_KEY_BASE" $auth.secretKeyBase "AWS_ACCESS_KEY_ID" .Values.s3.accessKeyId "AWS_SECRET_ACCESS_KEY" .Values.s3.secretAccessKey "RELEASE_COOKIE" .Values.cluster.cookie -}}
  {{- if $value -}}
    {{- $_ := set $credentials $key (toString $value | b64enc) -}}
  {{- end -}}
{{- end -}}
{{- toYaml $credentials -}}
{{- end -}}
