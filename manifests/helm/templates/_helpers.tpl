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

{{- /* [env key, values path, value] for each credential, in env order; read back with fromYamlArray. */ -}}
{{- define "slimserve.credentials" -}}
{{- $auth := .Values.auth -}}
{{- $credentials := list -}}
{{- range list (list "SLIMSERVE_PASSWORD" "auth.password" $auth.password) (list "SLIMSERVE_WRITE_PASSWORD" "auth.writePassword" $auth.writePassword) (list "SECRET_KEY_BASE" "auth.secretKeyBase" $auth.secretKeyBase) (list "AWS_ACCESS_KEY_ID" "s3.accessKeyId" .Values.s3.accessKeyId) (list "AWS_SECRET_ACCESS_KEY" "s3.secretAccessKey" .Values.s3.secretAccessKey) (list "RELEASE_COOKIE" "cluster.cookie" .Values.cluster.cookie) -}}
  {{- /* toString before toYaml, or YAML reads 12345678 back as 1.2345678e+07. */ -}}
  {{- $credentials = append $credentials (list (index . 0) (index . 1) (index . 2 | default "" | toString)) -}}
{{- end -}}
{{- toYaml $credentials -}}
{{- end -}}

{{- /* "true" or "": include returns a string, and "false" is truthy. */ -}}
{{- define "slimserve.cluster" -}}
{{- if gt (include "slimserve.replicas" . | int) 1 }}true{{ end -}}
{{- end -}}

{{- /* kindIs, not default, so an explicit 0 stays 0. */ -}}
{{- define "slimserve.replicas" -}}
{{- if kindIs "invalid" .Values.replicas -}}
{{- ternary 2 1 (eq (toString .Values.storage.backend) "s3") -}}
{{- else -}}
{{- int .Values.replicas -}}
{{- end -}}
{{- end -}}

{{- define "slimserve.labels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/component: server
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}
