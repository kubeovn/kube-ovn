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
Get IP-addresses of master nodes. If no nodes are returned, we assume this is
a dry-run/template call and return nothing.
*/}}
{{- define "kubeovn.nodeIPs" -}}
{{- $nodes := lookup "v1" "Node" "" "" -}}
{{- if $nodes -}}
{{- $ips := list -}}
{{- range $node := $nodes.items -}}
  {{- range $label, $value := $.Values.masterNodesLabels }}
  {{- if and (hasKey $node.metadata.labels $label) (or (eq ($value | toString) "") (eq (index $node.metadata.labels $label) ($value | toString))) -}}
    {{- range $address := $node.status.addresses -}}
      {{- if eq $address.type "InternalIP" -}}
        {{- $ips = append $ips $address.address -}}
        {{- break -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
  {{- end }}
{{- end -}}
{{- if and (eq (len $ips) 0) (not $.Values.masterNodes) -}}
  {{- fail (printf "No nodes found with label '%s'. Please check your masterNodesLabels configuration or ensure master nodes are properly labeled." $.Values.masterNodesLabels) -}}
{{- end -}}
{{ join "," $ips }}
{{- end -}}
{{- end -}}

{{/*
Build hardcodedRequired list for kube-ovn.affinities.nodeAffinity from masterNodesLabels.
Each label gets its own nodeSelectorTerm so multiple labels use OR semantics
(matching the kubeovn.nodeIPs helper which also uses OR).
Uses Exists operator for empty/nil-value labels and In for specific values.
*/}}
{{- define "kubeovn.masterNodeRequired" -}}
{{- $terms := list -}}
{{- range $key, $value := .Values.masterNodesLabels -}}
  {{- if eq ($value | toString) "" -}}
    {{- $terms = append $terms (dict "matchExpressions" (list (dict "key" $key "operator" "Exists"))) -}}
  {{- else -}}
    {{- $terms = append $terms (dict "matchExpressions" (list (dict "key" $key "operator" "In" "values" (list ($value | toString))))) -}}
  {{- end -}}
{{- end -}}
{{- $terms | toYaml -}}
{{- end -}}

{{/*
Number of master nodes
*/}}
{{- define "kubeovn.nodeCount" -}}
  {{- len (split "," ((join "," .Values.masterNodes) | default (include "kubeovn.nodeIPs" .))) }}
{{- end -}}

{{/*
Get IPs of master nodes from values
*/}}
{{- define "kubeovn.masterNodes" -}}
  {{- join "," .Values.masterNodes }}
{{- end -}}

{{/*
Environment variables used by the OVN NB/SB database server TLS setup.
*/}}
{{- define "kubeovn.ovnCentralTLSEnv" -}}
- name: ENABLE_SSL
  value: {{ .Values.networking.enableSsl | quote }}
- name: TLS_MIN_VERSION
  value: {{ .Values.networking.tlsMinVersion | quote }}
- name: TLS_MAX_VERSION
  value: {{ .Values.networking.tlsMaxVersion | quote }}
- name: TLS_CIPHER_SUITES
  value: {{ join "," .Values.networking.tlsCipherSuites | quote }}
{{- end -}}

{{/*
TLS arguments for kube-ovn components that expose HTTPS endpoints.
*/}}
{{- define "kubeovn.componentTLSArgs" -}}
{{- if .Values.networking.tlsMinVersion }}
- --tls-min-version={{ .Values.networking.tlsMinVersion }}
{{- end }}
{{- if .Values.networking.tlsMaxVersion }}
- --tls-max-version={{ .Values.networking.tlsMaxVersion }}
{{- end }}
{{- if .Values.networking.tlsCipherSuites }}
- --tls-cipher-suites={{ join "," .Values.networking.tlsCipherSuites }}
{{- end }}
{{- end -}}

{{- define "kubeovn.centralNamespace" -}}
{{- if .Values.central.separated.enabled -}}
{{- default .Values.namespace .Values.central.separated.namespace -}}
{{- else -}}
{{- .Values.namespace -}}
{{- end -}}
{{- end -}}

{{- define "kubeovn.centralReplicas" -}}
{{- if .Values.central.separated.enabled -}}
{{- .Values.central.separated.replicas -}}
{{- else -}}
{{- include "kubeovn.nodeCount" . -}}
{{- end -}}
{{- end -}}

{{- define "kubeovn.centralRaftAddresses" -}}
{{- $namespace := include "kubeovn.centralNamespace" . -}}
{{- $addresses := list -}}
{{- range $i := until (int .Values.central.separated.replicas) -}}
{{- $addresses = append $addresses (printf "ovn-central-%d.ovn-central.%s.svc" $i $namespace) -}}
{{- end -}}
{{- join "," $addresses -}}
{{- end -}}

{{- define "kubeovn.ovnDbAddresses" -}}
{{- if .Values.central.separated.enabled -}}
{{- if not .Values.central.separated.externalAddresses -}}
{{- fail "central.separated.externalAddresses must be set when central.separated.enabled is true" -}}
{{- end -}}
{{- join "," .Values.central.separated.externalAddresses -}}
{{- else -}}
{{- include "kubeovn.masterNodes" . | default (include "kubeovn.nodeIPs" .) -}}
{{- end -}}
{{- end -}}

{{- define "kubeovn.ovnNbPort" -}}
{{- if .Values.central.separated.enabled -}}
{{- .Values.central.separated.service.nbNodePort -}}
{{- else -}}
6641
{{- end -}}
{{- end -}}

{{- define "kubeovn.ovnSbPort" -}}
{{- if .Values.central.separated.enabled -}}
{{- .Values.central.separated.service.sbNodePort -}}
{{- else -}}
6642
{{- end -}}
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
    {{- $.Values.ovsOvn.updateStrategy.type -}}
  {{- end -}}
{{- end -}}


{{- define "kubeovn.runAsUser" -}}
  {{- if $.Values.features.enableOvnIpsec -}}
    0
  {{- else -}}
    65534
  {{- end -}}
{{- end -}}

{{- define "kubeovn.imageSpec" -}}
  {{- $root := .root -}}
  {{- $image := .image | default dict -}}
  {{- $address := get $image "registry" | default $root.Values.global.registry.address -}}
  {{- $repository := .defaultRepository | default $root.Values.global.images.kubeovn.repository -}}
  {{- $tag := .defaultTag | default $root.Values.global.images.kubeovn.tag -}}
  {{- $prefix := "" -}}
  {{- if $address -}}
    {{- $prefix = printf "%s/" $address -}}
  {{- end -}}
  {{- dict
      "address" $address
      "prefix" $prefix
      "repository" (get $image "repository" | default $repository)
      "tag" (get $image "tag" | default $tag)
      "pullPolicy" (get $image "pullPolicy" | default $root.Values.image.pullPolicy)
    | toYaml -}}
{{- end -}}

{{/*
Merge hardcoded node affinity expressions with user-provided values.
Usage: include "kube-ovn.affinities.nodeAffinity" (dict "hardcodedPreferred" $hardcodedPreferred "hardcodedRequired" $hardcodedRequired "userPreferred" .Values.component.nodeAffinity.preferredDuringSchedulingIgnoredDuringExecution "userRequired" .Values.component.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution)
*/}}
{{- define "kube-ovn.affinities.nodeAffinity" -}}
{{- $hardcodedPreferred := .hardcodedPreferred | default list -}}
{{- $hardcodedRequired := .hardcodedRequired | default list -}}
{{- $userPreferred := .userPreferred | default list -}}
{{- $userRequired := .userRequired | default list -}}
{{- $mergedPreferred := concat $hardcodedPreferred $userPreferred -}}
{{- $mergedRequired := concat $hardcodedRequired $userRequired -}}
{{- if or $mergedPreferred $mergedRequired -}}
nodeAffinity:
  {{- if $mergedPreferred }}
  preferredDuringSchedulingIgnoredDuringExecution:
    {{- range $mergedPreferred }}
    - preference:
        matchExpressions:
          {{- toYaml .matchExpressions | nindent 10 }}
      weight: {{ .weight | default 100 }}
    {{- end }}
  {{- end }}
  {{- if $mergedRequired }}
  requiredDuringSchedulingIgnoredDuringExecution:
    nodeSelectorTerms:
      {{- range $mergedRequired }}
      - matchExpressions:
          {{- toYaml .matchExpressions | nindent 8 }}
      {{- end }}
  {{- end }}
{{- end -}}
{{- end -}}
