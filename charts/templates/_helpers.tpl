{{- define "kubeovn.ovs-ovn.updateStrategy" -}}
  {{- $ds := lookup "apps/v1" "DaemonSet" $.Values.namespace "ovs-ovn" -}}
  {{- if $ds -}}
    {{- $updateStrategy := $ds.spec.updateStrategy.type }}
    {{- $imageVersion := index ((index $ds.spec.template.spec.containers 0).image | splitList ":") 1 | trimPrefix "v" }}
    {{- if or (eq $updateStrategy "RollingUpdate") (semverCompare ">= 1.12.0" $imageVersion) -}}
      RollingUpdate
    {{- else -}}
      OnDelete
    {{- end -}}
  {{- else -}}
    RollingUpdate
  {{- end -}}
{{- end -}}
