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
{{- if and (eq (len $ips) 0) (not $.Values.MASTER_NODES) -}}
  {{- fail (printf "No nodes found with label '%s'. Please check your MASTER_NODES_LABEL configuration or ensure master nodes are properly labeled." $.Values.MASTER_NODES_LABEL) -}}
{{- end -}}
{{ join "," $ips }}
{{- end -}}
{{- end -}}

{{/*
Number of master nodes
*/}}
{{- define "kubeovn.nodeCount" -}}
  {{- len (split "," (.Values.MASTER_NODES | default (include "kubeovn.nodeIPs" .))) }}
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
