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
Number of Kubernetes nodes, falling back to MASTER_NODES for offline rendering.
*/}}
{{- define "kubeovn.k8sNodeCount" -}}
{{- $nodes := lookup "v1" "Node" "" "" -}}
{{- if and $nodes $nodes.items -}}
{{- len $nodes.items -}}
{{- else -}}
{{- include "kubeovn.nodeCount" . -}}
{{- end -}}
{{- end -}}

{{/*
Replica count for the ovn-central Deployment. Single mode always uses 1;
cluster mode uses one replica per master node.
*/}}
{{- define "kubeovn.ovnCentralReplicas" -}}
{{- if index .Values "ovn-central" "hcp" "enabled" -}}
{{- index .Values "ovn-central" "hcp" "replicas" -}}
{{- else if eq .Values.OVN_CENTRAL_MODE "single" -}}
1
{{- else -}}
{{- include "kubeovn.nodeCount" . -}}
{{- end -}}
{{- end -}}

{{- define "kubeovn.centralNamespace" -}}
{{- if index .Values "ovn-central" "hcp" "enabled" -}}
{{- default .Values.namespace (index .Values "ovn-central" "hcp" "namespace") -}}
{{- else -}}
{{- .Values.namespace -}}
{{- end -}}
{{- end -}}

{{- define "kubeovn.centralRaftAddresses" -}}
{{- $namespace := include "kubeovn.centralNamespace" . -}}
{{- $addresses := list -}}
{{- range $i := until (int (index .Values "ovn-central" "hcp" "replicas")) -}}
{{- $addresses = append $addresses (printf "ovn-central-%d.ovn-central.%s.svc" $i $namespace) -}}
{{- end -}}
{{- join "," $addresses -}}
{{- end -}}

{{/*
Environment variables used by the OVN NB/SB database server TLS setup.
*/}}
{{- define "kubeovn.ovnCentralTLSEnv" -}}
- name: ENABLE_SSL
  value: {{ .Values.networking.ENABLE_SSL | quote }}
- name: TLS_MIN_VERSION
  value: {{ .Values.networking.TLS_MIN_VERSION | quote }}
- name: TLS_MAX_VERSION
  value: {{ .Values.networking.TLS_MAX_VERSION | quote }}
- name: TLS_CIPHER_SUITES
  value: {{ join "," .Values.networking.TLS_CIPHER_SUITES | quote }}
{{- end -}}

{{/*
TLS arguments for kube-ovn components that expose HTTPS endpoints.
*/}}
{{- define "kubeovn.componentTLSArgs" -}}
{{- if .Values.networking.TLS_MIN_VERSION }}
- --tls-min-version={{ .Values.networking.TLS_MIN_VERSION }}
{{- end }}
{{- if .Values.networking.TLS_MAX_VERSION }}
- --tls-max-version={{ .Values.networking.TLS_MAX_VERSION }}
{{- end }}
{{- if .Values.networking.TLS_CIPHER_SUITES }}
{{- range .Values.networking.TLS_CIPHER_SUITES }}
- --tls-cipher-suites={{ . }}
{{- end }}
{{- end }}
{{- end -}}

{{/*
Kube-OVN TLS is owned by the management cluster in dataPlaneOnly installs.
Disable local rotation there so a tenant cluster cannot replace the shared CA.
*/}}
{{- define "kubeovn.kubeOVNTLSRotationInterval" -}}
{{- if eq .Values.installMode "dataPlaneOnly" -}}
0
{{- else -}}
{{ .Values.networking.KUBE_OVN_TLS_ROTATION_INTERVAL }}
{{- end -}}
{{- end -}}

{{/*
Replica count for the kube-ovn-controller Deployment.
dataPlaneOnly runs against external HCP ovn-central, so cap controller
replicas at two while allowing single-node clusters to run one.
*/}}
{{- define "kubeovn.controllerReplicas" -}}
{{- $override := dig "replicas" nil (index .Values "kube-ovn-controller") -}}
{{- if eq .Values.installMode "dataPlaneOnly" -}}
{{- min 2 (include "kubeovn.k8sNodeCount" . | int) -}}
{{- else if $override -}}
{{ $override }}
{{- else -}}
{{- include "kubeovn.nodeCount" . -}}
{{- end -}}
{{- end -}}

{{/*
Value of the NODE_IPS / OVN_DB_IPS env variable.
  - dataPlaneOnly: invalid; workload components use OVN_NB_ADDR/OVN_SB_ADDR.
  - single (control-plane / full): emit empty so start-db.sh takes the
    standalone path and clients fall back to Service ClusterIP via the
    auto-injected OVN_*_SERVICE_HOST env vars.
  - cluster (default raft): emit comma-separated master node IPs.
*/}}
{{- define "kubeovn.ovnCentralNodeIPs" -}}
{{- if index .Values "ovn-central" "hcp" "enabled" -}}
{{- include "kubeovn.centralRaftAddresses" . -}}
{{- else if eq .Values.installMode "dataPlaneOnly" -}}
{{- fail "installMode=dataPlaneOnly uses OVN_NB_ADDR/OVN_SB_ADDR; do not render OVN_DB_IPS" -}}
{{- else if eq .Values.OVN_CENTRAL_MODE "single" -}}
{{- else -}}
{{ .Values.MASTER_NODES | default (include "kubeovn.nodeIPs" .) }}
{{- end -}}
{{- end -}}

{{- define "kubeovn.externalOvnNbAddress" -}}
{{- $endpoint := required "installMode=dataPlaneOnly requires externalOvnCentral.nbEndpoint" .Values.externalOvnCentral.nbEndpoint -}}
tcp:{{ include "kubeovn.externalOvnHost" $endpoint }}:{{ .Values.externalOvnCentral.nbPort | default 6641 }}
{{- end -}}

{{- define "kubeovn.externalOvnSbAddress" -}}
{{- $endpoint := required "installMode=dataPlaneOnly requires externalOvnCentral.sbEndpoint" .Values.externalOvnCentral.sbEndpoint -}}
tcp:{{ include "kubeovn.externalOvnHost" $endpoint }}:{{ .Values.externalOvnCentral.sbPort | default 6642 }}
{{- end -}}

{{- define "kubeovn.externalOvnHost" -}}
{{- if and (contains ":" .) (not (hasPrefix "[" .)) -}}
[{{ . }}]
{{- else -}}
{{ . }}
{{- end -}}
{{- end -}}

{{- define "kubeovn.ovnNbAddress" -}}
{{- if not (index .Values "ovn-central" "hcp" "nbAddress") -}}
{{- fail "ovn-central.hcp.nbAddress must be set when ovn-central.hcp.enabled is true" -}}
{{- end -}}
{{- index .Values "ovn-central" "hcp" "nbAddress" -}}
{{- end -}}

{{- define "kubeovn.ovnSbAddress" -}}
{{- if not (index .Values "ovn-central" "hcp" "sbAddress") -}}
{{- fail "ovn-central.hcp.sbAddress must be set when ovn-central.hcp.enabled is true" -}}
{{- end -}}
{{- index .Values "ovn-central" "hcp" "sbAddress" -}}
{{- end -}}

{{/*
Validate the ovn-central.service block. Currently the only invariant we
enforce is that LoadBalancer service type must come with an explicit
loadBalancerIP, because ovn-nb / ovn-sb / ovn-northd should land on the same
stable VIP when exposed through that Service type. Without it cloud LBs may
allocate three different IPs unexpectedly. Use {{- include "kubeovn.validateService" . }}
anywhere a Service is rendered so the validation runs on every template.
*/}}
{{- define "kubeovn.validateService" -}}
{{- $svc := index .Values "ovn-central" "service" -}}
{{- if eq $svc.type "LoadBalancer" -}}
{{- if not $svc.loadBalancerIP -}}
{{- fail "ovn-central.service.type=LoadBalancer requires ovn-central.service.loadBalancerIP to be set so ovn-nb / ovn-sb / ovn-northd share a stable VIP. Pick a VIP, configure your LB controller to assign it (MetalLB allow-shared-ip annotation is emitted automatically), then re-run helm." -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Port the agents/controller should use to connect to ovn-nb. In dataPlaneOnly
mode this picks up externalOvnCentral.nbPort so NodePort or non-default
LoadBalancer port mappings work. Other modes use the in-cluster Service port
6641.
*/}}
{{- define "kubeovn.ovnNbPort" -}}
{{- if eq .Values.installMode "dataPlaneOnly" -}}
{{ .Values.externalOvnCentral.nbPort | default 6641 }}
{{- else -}}
6641
{{- end -}}
{{- end -}}

{{/*
Port the agents/controller should use to connect to ovn-sb. Mirror of
kubeovn.ovnNbPort for the southbound DB.
*/}}
{{- define "kubeovn.ovnSbPort" -}}
{{- if eq .Values.installMode "dataPlaneOnly" -}}
{{ .Values.externalOvnCentral.sbPort | default 6642 }}
{{- else -}}
6642
{{- end -}}
{{- end -}}

{{/*
Render gate for control-plane resources (ovn-central + Services + its RBAC).
Emits "true" when this Helm release should render control-plane resources;
empty otherwise. Use with {{- if include "kubeovn.renderControlPlane" . }}.
*/}}
{{- define "kubeovn.renderControlPlane" -}}
{{- if or (eq .Values.installMode "full") (eq .Values.installMode "controlPlaneOnly") -}}
true
{{- end -}}
{{- end -}}

{{/*
Render gate for data-plane resources (CRDs + kube-ovn-controller + ovs-ovn +
kube-ovn-cni + kube-ovn-pinger + kube-ovn-monitor + their RBAC).
*/}}
{{- define "kubeovn.renderDataPlane" -}}
{{- if or (eq .Values.installMode "full") (eq .Values.installMode "dataPlaneOnly") -}}
true
{{- end -}}
{{- end -}}

{{/*
Render gate for components that only make sense in a single-cluster install:
- ovn-dpdk DaemonSet (start-ovs-dpdk-v2.sh still talks to OVN_SB_SERVICE_HOST,
  no externalOvnCentral support yet)
- pre-upgrade-ovs-ovn / upgrade-ovs-ovn hooks (upgrade-ovs.sh waits on a local
  deploy/ovn-central, so it fails on tenant-only installs)
Use `kubeovn.renderFullOnly` when the resource is not yet ready for the
Kamaji-style split deployments.
*/}}
{{- define "kubeovn.renderFullOnly" -}}
{{- if eq .Values.installMode "full" -}}
true
{{- end -}}
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
