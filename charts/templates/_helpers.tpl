{{/*
Get IP-addresses of master nodes
*/}}
{{- define "kubeovn.nodeIPs" -}}
{{- $nodes := lookup "v1" "Node" "" "" -}}
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
{{ join "," $ips }}
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
    {{- $updateStrategy := $ds.spec.updateStrategy.type }}
    {{- $imageVersion := (index $ds.spec.template.spec.containers 0).image | splitList ":" | last | trimPrefix "v" }}
    {{- if or (eq $updateStrategy "RollingUpdate") (semverCompare ">= 1.12.0" $imageVersion) -}}
      RollingUpdate
    {{- else -}}
      OnDelete
    {{- end -}}
  {{- else -}}
    RollingUpdate
  {{- end -}}
{{- end -}}
