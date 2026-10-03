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
Names for resources owned by this release. Keeping these in helpers makes
every resource identity and cross-reference follow fullnameOverride.
*/}}
{{- define "kubeovn.resourceName" -}}
{{- $suffix := .suffix -}}
{{- $base := include "kubeovn.fullname" .context | trunc (int (sub 63 (add (len $suffix) 1))) | trimSuffix "-" -}}
{{- printf "%s-%s" $base $suffix | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubeovn.centralName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovn-central") | trunc 61 | trimSuffix "-" }}{{- end -}}
{{- define "kubeovn.centralNBName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovn-nb") }}{{- end -}}
{{- define "kubeovn.centralSBName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovn-sb") }}{{- end -}}
{{- define "kubeovn.centralNorthdName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovn-northd") }}{{- end -}}
{{- define "kubeovn.centralServiceAccountName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovn-central") }}{{- end -}}
{{- define "kubeovn.controllerName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "controller") }}{{- end -}}
{{- define "kubeovn.agentName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "cni") }}{{- end -}}
{{- define "kubeovn.pingerName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "pinger") }}{{- end -}}
{{- define "kubeovn.monitorName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "monitor") }}{{- end -}}
{{- define "kubeovn.ovsOvnName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovs-ovn") }}{{- end -}}
{{- define "kubeovn.ovsOvnDpdkName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovs-ovn-dpdk") }}{{- end -}}
{{- define "kubeovn.icName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ic-controller") }}{{- end -}}
{{- define "kubeovn.speakerName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "speaker") }}{{- end -}}
{{- define "kubeovn.webhookName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "webhook") }}{{- end -}}
{{- define "kubeovn.tlsSecretName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "tls") }}{{- end -}}
{{- define "kubeovn.ovnServiceAccountName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovn") }}{{- end -}}
{{- define "kubeovn.ovsServiceAccountName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "ovn-ovs") }}{{- end -}}
{{- define "kubeovn.appServiceAccountName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "app") }}{{- end -}}
{{- define "kubeovn.agentServiceAccountName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "cni") }}{{- end -}}
{{- define "kubeovn.ovnClusterRoleName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "system-ovn") }}{{- end -}}
{{- define "kubeovn.ovsClusterRoleName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "system-ovn-ovs") }}{{- end -}}
{{- define "kubeovn.appClusterRoleName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "system-app") }}{{- end -}}
{{- define "kubeovn.agentClusterRoleName" -}}{{ include "kubeovn.resourceName" (dict "context" . "suffix" "system-cni") }}{{- end -}}

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
{{- range .Values.networking.tlsCipherSuites }}
- --tls-cipher-suites={{ . }}
{{- end }}
{{- end }}
{{- end -}}

{{- define "kubeovn.centralNamespace" -}}
{{- if .Values.central.hcp.enabled -}}
{{- default .Values.namespace .Values.central.hcp.namespace -}}
{{- else -}}
{{- .Values.namespace -}}
{{- end -}}
{{- end -}}

{{- define "kubeovn.centralReplicas" -}}
{{- if .Values.central.hcp.enabled -}}
{{- .Values.central.hcp.replicas -}}
{{- else -}}
{{- include "kubeovn.nodeCount" . -}}
{{- end -}}
{{- end -}}

{{- define "kubeovn.centralRaftAddresses" -}}
{{- $namespace := include "kubeovn.centralNamespace" . -}}
{{- $centralName := include "kubeovn.centralName" . -}}
{{- $addresses := list -}}
{{- range $i := until (int .Values.central.hcp.replicas) -}}
{{- $addresses = append $addresses (printf "%s-%d.%s.%s.svc" $centralName $i $centralName $namespace) -}}
{{- end -}}
{{- join "," $addresses -}}
{{- end -}}

{{- define "kubeovn.ovnDbAddresses" -}}
{{- include "kubeovn.masterNodes" . | default (include "kubeovn.nodeIPs" .) -}}
{{- end -}}

{{- define "kubeovn.ovnNbAddress" -}}
{{- if not .Values.central.hcp.nbAddress -}}
{{- fail "central.hcp.nbAddress must be set when central.hcp.enabled is true" -}}
{{- end -}}
{{- .Values.central.hcp.nbAddress -}}
{{- end -}}

{{- define "kubeovn.ovnSbAddress" -}}
{{- if not .Values.central.hcp.sbAddress -}}
{{- fail "central.hcp.sbAddress must be set when central.hcp.enabled is true" -}}
{{- end -}}
{{- .Values.central.hcp.sbAddress -}}
{{- end -}}

{{- define "kubeovn.ovs-ovn.updateStrategy" -}}
  {{- $ds := lookup "apps/v1" "DaemonSet" $.Values.namespace (include "kubeovn.ovsOvnName" $) -}}
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
