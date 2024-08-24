{/*
Expand the name of the chart.
*/}}
{{- define "kubeovn.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "kubeovn.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kubeovn.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kubeovn.labels" -}}
helm.sh/chart: {{ include "kubeovn.chart" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}


{{/*
Create the name of the service account to use
*/}}
{{- define "kubeovn.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "kubeovn.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}


{{/*
Get IP-addresses of master nodes
*/}}
{{- define "kubeovn.nodeIPs" -}}
{{- $nodes := lookup "v1" "Node" "" "" -}}
{{- $ips := list -}}
{{- range $node := $nodes.items -}}
  {{- $label := splitList "=" $.Values.masterNodesLabel}}
  {{- $key := index $label 0 }}
  {{- $val := "" }}
  {{- if eq (len $label) 2 }}
  {{- $val = index $label 1 }}
  {{- end }}
  {{- if eq (index $node.metadata.labels $key) $val -}}
    {{- range $address := $node.status.addresses -}}
      {{- if eq $address.type "InternalIP" -}}
        {{- $ips = append $ips $address.address -}}
        {{- break -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{ join "," $ips }}
{{- end -}}

{{/*
Number of master nodes
*/}}
{{- define "kubeovn.nodeCount" -}}
  {{- len (split "," (.Values.masterNodes| default (include "kubeovn.nodeIPs" .))) }}
{{- end -}}

{{- define "kubeovn.ovs-ovn.updateStrategy" -}}
  {{- $ds := lookup "apps/v1" "DaemonSet" $.Values.namespace "ovs-ovn" -}}
  {{- if $ds -}}
    {{- if eq $ds.spec.updateStrategy.type "RollingUpdate" -}}
      RollingUpdate
    {{- else -}}
      {{- $chartVersion := index $ds.metadata.annotations "chart-version" }}
      {{- $newChartVersion := printf "%s-%s" .Chart.Name .Chart.Version }}
      {{- $imageVersion := (index $ds.spec.template.spec.containers 0).image | splitList ":" | last | trimPrefix "v" -}}
      {{- $versionRegex := `^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)` -}}
      {{- if and (ne $newChartVersion $chartVersion) (regexMatch $versionRegex $imageVersion) -}}
        {{- if regexFind $versionRegex $imageVersion | semverCompare ">= 1.12.0" -}}
          RollingUpdate
        {{- else -}}
          OnDelete
        {{- end -}}
      {{- else -}}
        OnDelete
      {{- end -}}
    {{- end -}}
  {{- else -}}
    RollingUpdate
  {{- end -}}
{{- end -}}

{{- define "kubeovn.ovn.versionCompatibility" -}}
  {{- $ds := lookup "apps/v1" "DaemonSet" $.Values.namespace "ovs-ovn" -}}
  {{- if $ds -}}
    {{- $chartVersion := index $ds.metadata.annotations "chart-version" }}
    {{- $newChartVersion := printf "%s-%s" .Chart.Name .Chart.Version }}
    {{- $imageVersion := (index $ds.spec.template.spec.containers 0).image | splitList ":" | last | trimPrefix "v" -}}
    {{- $versionRegex := `^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)` -}}
    {{- if and (ne $newChartVersion $chartVersion) (regexMatch $versionRegex $imageVersion) -}}
      {{- if regexFind $versionRegex $imageVersion | semverCompare ">= 1.13.0" -}}
        24.03
      {{- else if regexFind $versionRegex $imageVersion | semverCompare ">= 1.12.0" -}}
        22.12
      {{- else if regexFind $versionRegex $imageVersion | semverCompare ">= 1.11.0" -}}
        22.03
      {{- else -}}
        21.06
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}

{{- define "kubeovn.runAsUser" -}}
  {{- if $.Values.features.ENABLE_OVN_IPSEC -}}
    0
  {{- else -}}
    65534
  {{- end -}}
{{- end -}}
