{{/*
Get IP-addresses of master nodes
*/}}
{{- define "kubeovn.nodeIPs" -}}
{{- $nodes := lookup "v1" "Node" "" "" -}}
{{- $ips := list -}}
{{- range $node := $nodes.items -}}
  {{- if eq (index $node.metadata.labels "kube-ovn/role") "master" -}}
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
