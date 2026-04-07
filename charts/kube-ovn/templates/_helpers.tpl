{{/*
Get IP-addresses of master nodes. If no nodes are returned, we assume this is
a dry-run/template call and return nothing.
*/}}
{{- define "kubeovn.nodeIPs" -}}
{{- $nodes := lookup "v1" "Node" "" "" -}}
{{- if $nodes -}}
{{- $ips := list -}}
{{- range $node := $nodes.items -}}
  {{- $label := splitList "=" $.Values.MASTER_NODES_LABEL }}
  {{- $key := index $label 0 }}
  {{- $val := "" }}
  {{- if gt (len $label) 1 }}
  {{- $val = join "=" (rest $label) }}
  {{- end }}
  {{- if and (hasKey $node.metadata.labels $key) (or (eq $val "") (eq (index $node.metadata.labels $key) $val)) -}}
    {{- range $address := $node.status.addresses -}}
      {{- if eq $address.type "InternalIP" -}}
        {{- $ips = append $ips $address.address -}}
        {{- break -}}
      {{- end -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{- if and (eq (len $ips) 0) (not $.Values.MASTER_NODES) -}}
  {{- fail (printf "No nodes found with label '%s'. Please check your MASTER_NODES_LABEL configuration or ensure master nodes are properly labeled." $.Values.MASTER_NODES_LABEL) -}}
{{- end -}}
{{ join "," $ips }}
{{- end -}}
{{- end -}}

{{/*
Render nodeAffinity for master node scheduling.
Uses Exists operator when MASTER_NODES_LABEL has no value or empty value
(matches any value), and In operator when a specific value is given.
Handles "key", "key=value", "key=" and "key=val=ue" formats correctly.
*/}}
{{- define "kubeovn.masterNodeAffinity" -}}
{{- $parts := splitList "=" .Values.MASTER_NODES_LABEL -}}
{{- $key := index $parts 0 -}}
{{- $val := "" -}}
{{- if gt (len $parts) 1 -}}
  {{- $val = join "=" (rest $parts) -}}
{{- end -}}
nodeAffinity:
  requiredDuringSchedulingIgnoredDuringExecution:
    nodeSelectorTerms:
      - matchExpressions:
          - key: {{ $key }}
          {{- if ne $val "" }}
            operator: In
            values:
              - {{ $val | quote }}
          {{- else }}
            operator: Exists
          {{- end }}
{{- end -}}

{{/*
Number of master nodes
*/}}
{{- define "kubeovn.nodeCount" -}}
  {{- len (split "," (.Values.MASTER_NODES | default (include "kubeovn.nodeIPs" .))) }}
{{- end -}}

{{/*
Determine the updateStrategy type for the ovs-ovn DaemonSet.
If ovs-ovn.updateStrategy.type is set, use it directly.
Otherwise, auto-detect based on the currently deployed DaemonSet.
*/}}
{{- define "kubeovn.ovs-ovn.updateStrategy" -}}
  {{- $updateStrategy := index $.Values "ovs-ovn" "updateStrategy" -}}
  {{- $desiredStrategy := "" -}}
  {{- if $updateStrategy -}}
    {{- $desiredStrategy = index $updateStrategy "type" -}}
  {{- end -}}
  {{- if $desiredStrategy -}}
    {{- $desiredStrategy -}}
  {{- else -}}
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
{{- end -}}

{{- define "kubeovn.ovn.versionCompatibility" -}}
  {{- $ds := lookup "apps/v1" "DaemonSet" $.Values.namespace "ovs-ovn" -}}
  {{- if $ds -}}
    {{- $chartVersion := index $ds.metadata.annotations "chart-version" }}
    {{- $newChartVersion := printf "%s-%s" .Chart.Name .Chart.Version }}
    {{- $imageVersion := (index $ds.spec.template.spec.containers 0).image | splitList ":" | last | trimPrefix "v" -}}
    {{- $versionRegex := `^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)` -}}
    {{- if and (ne $newChartVersion $chartVersion) (regexMatch $versionRegex $imageVersion) -}}
      {{- if regexFind $versionRegex $imageVersion | semverCompare ">= 1.15.0" -}}
        25.03
      {{- else if regexFind $versionRegex $imageVersion | semverCompare ">= 1.13.0" -}}
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
  {{- if $.Values.func.ENABLE_OVN_IPSEC -}}
    0
  {{- else -}}
    65534
  {{- end -}}
{{- end -}}
