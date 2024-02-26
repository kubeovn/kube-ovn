#!/usr/bin/env bash
set -euo pipefail

IPV6=${IPV6:-false}
DUAL_STACK=${DUAL_STACK:-false}
ENABLE_SSL=${ENABLE_SSL:-false}
ENABLE_VLAN=${ENABLE_VLAN:-false}
CHECK_GATEWAY=${CHECK_GATEWAY:-true}
LOGICAL_GATEWAY=${LOGICAL_GATEWAY:-false}
U2O_INTERCONNECTION=${U2O_INTERCONNECTION:-false}
ENABLE_MIRROR=${ENABLE_MIRROR:-false}
VLAN_NIC=${VLAN_NIC:-}
HW_OFFLOAD=${HW_OFFLOAD:-false}
ENABLE_LB=${ENABLE_LB:-true}
ENABLE_NP=${ENABLE_NP:-true}
ENABLE_EIP_SNAT=${ENABLE_EIP_SNAT:-true}
LS_DNAT_MOD_DL_DST=${LS_DNAT_MOD_DL_DST:-true}
LS_CT_SKIP_DST_LPORT_IPS=${LS_CT_SKIP_DST_LPORT_IPS:-true}
ENABLE_EXTERNAL_VPC=${ENABLE_EXTERNAL_VPC:-true}
CNI_CONFIG_PRIORITY=${CNI_CONFIG_PRIORITY:-01}
ENABLE_LB_SVC=${ENABLE_LB_SVC:-false}
ENABLE_NAT_GW=${ENABLE_NAT_GW:-false}
ENABLE_KEEP_VM_IP=${ENABLE_KEEP_VM_IP:-true}
ENABLE_ARP_DETECT_IP_CONFLICT=${ENABLE_ARP_DETECT_IP_CONFLICT:-true}
NODE_LOCAL_DNS_IP=${NODE_LOCAL_DNS_IP:-}
ENABLE_IC=${ENABLE_IC:-$(kubectl get node --show-labels | grep -qw "ovn.kubernetes.io/ic-gw" && echo true || echo false)}
# exchange link names of OVS bridge and the provider nic
# in the default provider-network
EXCHANGE_LINK_NAME=${EXCHANGE_LINK_NAME:-false}
# The nic to support container network can be a nic name or a group of regex
# separated by comma, if empty will use the nic that the default route use
IFACE=${IFACE:-}
# Specifies the name of the dpdk tunnel iface.
# Note that the dpdk tunnel iface and tunnel ip cidr should be diffierent with Kubernetes api cidr, otherwise the route will be a problem.
DPDK_TUNNEL_IFACE=${DPDK_TUNNEL_IFACE:-br-phy}
ENABLE_BIND_LOCAL_IP=${ENABLE_BIND_LOCAL_IP:-true}
ENABLE_TPROXY=${ENABLE_TPROXY:-false}
OVS_VSCTL_CONCURRENCY=${OVS_VSCTL_CONCURRENCY:-100}
ENABLE_COMPACT=${ENABLE_COMPACT:-false}

# debug
DEBUG_WRAPPER=${DEBUG_WRAPPER:-}

KUBELET_DIR=${KUBELET_DIR:-/var/lib/kubelet}
LOG_DIR=${LOG_DIR:-/var/log}

CNI_CONF_DIR="/etc/cni/net.d"
CNI_BIN_DIR="/opt/cni/bin"

REGISTRY="docker.io/kubeovn"
VPC_NAT_IMAGE="vpc-nat-gateway"
VERSION="v1.13.0"
IMAGE_PULL_POLICY="IfNotPresent"
POD_CIDR="10.16.0.0/16"                     # Do NOT overlap with NODE/SVC/JOIN CIDR
POD_GATEWAY="10.16.0.1"
SVC_CIDR="10.96.0.0/12"                     # Do NOT overlap with NODE/POD/JOIN CIDR
JOIN_CIDR="100.64.0.0/16"                   # Do NOT overlap with NODE/POD/SVC CIDR
PINGER_EXTERNAL_ADDRESS="1.1.1.1"           # Pinger check external ip probe
PINGER_EXTERNAL_DOMAIN="alauda.cn."         # Pinger check external domain probe
SVC_YAML_IPFAMILYPOLICY=""
if [ "$IPV6" = "true" ]; then
  POD_CIDR="fd00:10:16::/112"               # Do NOT overlap with NODE/SVC/JOIN CIDR
  POD_GATEWAY="fd00:10:16::1"
  SVC_CIDR="fd00:10:96::/112"               # Do NOT overlap with NODE/POD/JOIN CIDR
  JOIN_CIDR="fd00:100:64::/112"             # Do NOT overlap with NODE/POD/SVC CIDR
  PINGER_EXTERNAL_ADDRESS="2606:4700:4700::1111"
  PINGER_EXTERNAL_DOMAIN="google.com."
fi
if [ "$DUAL_STACK" = "true" ]; then
  POD_CIDR="10.16.0.0/16,fd00:10:16::/112"               # Do NOT overlap with NODE/SVC/JOIN CIDR
  POD_GATEWAY="10.16.0.1,fd00:10:16::1"
  SVC_CIDR="10.96.0.0/12,fd00:10:96::/112"               # Do NOT overlap with NODE/POD/JOIN CIDR
  JOIN_CIDR="100.64.0.0/16,fd00:100:64::/112"            # Do NOT overlap with NODE/POD/SVC CIDR
  PINGER_EXTERNAL_ADDRESS="1.1.1.1,2606:4700:4700::1111"
  PINGER_EXTERNAL_DOMAIN="google.com."
  SVC_YAML_IPFAMILYPOLICY="ipFamilyPolicy: PreferDualStack"
fi

EXCLUDE_IPS=""                                    # EXCLUDE_IPS for default subnet
LABEL="node-role.kubernetes.io/control-plane"     # The node label to deploy OVN DB
DEPRECATED_LABEL="node-role.kubernetes.io/master" # The node label to deploy OVN DB in earlier versions
NETWORK_TYPE="geneve"                             # geneve or vlan
TUNNEL_TYPE="geneve"                              # geneve, vxlan or stt. ATTENTION: some networkpolicy cannot take effect when using vxlan and stt need custom compile ovs kernel module
POD_NIC_TYPE="veth-pair"                          # veth-pair or internal-port

# VLAN Config only take effect when NETWORK_TYPE is vlan
VLAN_INTERFACE_NAME=""
VLAN_ID="100"

if [ "$ENABLE_VLAN" = "true" ]; then
  NETWORK_TYPE="vlan"
  # ENABLE_EIP_SNAT is only supported when you use vpc, vlan not support
  ENABLE_EIP_SNAT=${ENABLE_EIP_SNAT:-false}
  if [ "$VLAN_NIC" != "" ]; then
    VLAN_INTERFACE_NAME="$VLAN_NIC"
  fi
fi

# hybrid dpdk
HYBRID_DPDK="false"

# DPDK
DPDK="false"
DPDK_SUPPORTED_VERSIONS=("19.11")
DPDK_VERSION=""
DPDK_CPU="1000m"                        # Default CPU configuration for if --dpdk-cpu flag is not included
DPDK_MEMORY="2Gi"                       # Default Memory configuration for it --dpdk-memory flag is not included

# performance
MODULES="kube_ovn_fastpath.ko"
RPMS="openvswitch-kmod"
GC_INTERVAL=360
INSPECT_INTERVAL=20

display_help() {
    echo "Usage: $0 [option...]"
    echo
    echo "  -h, --help               Print Help (this message) and exit"
    echo "  --with-hybrid-dpdk       Install Kube-OVN with nodes which run ovs-dpdk or ovs-kernel"
    echo "  --with-dpdk=<version>    Install Kube-OVN with OVS-DPDK instead of kernel OVS"
    echo "  --dpdk-cpu=<amount>m     Configure DPDK to use a specific amount of CPU"
    echo "  --dpdk-memory=<amount>Gi Configure DPDK to use a specific amount of memory"
    echo
    exit 0
}

if [ -n "${1-}" ]
then
  set +u
  while :; do
    case $1 in
      -h|--help)
        display_help
      ;;
      --with-hybrid-dpdk)
      HYBRID_DPDK="true"
      ;;
      --with-dpdk=*)
        DPDK=true
        DPDK_VERSION="${1#*=}"
        if [[ ! "${DPDK_SUPPORTED_VERSIONS[*]}" = "${DPDK_VERSION}" ]] || [[ -z "${DPDK_VERSION}" ]]; then
          echo "Unsupported DPDK version: ${DPDK_VERSION}"
          echo "Supported DPDK versions: ${DPDK_SUPPORTED_VERSIONS[*]}"
          exit 1
        fi
      ;;
      --dpdk-cpu=*)
        DPDK_CPU="${1#*=}"
        if [[ $DPDK_CPU =~ ^[0-9]+(m)$ ]]
        then
           echo "CPU $DPDK_CPU"
        else
           echo "$DPDK_CPU is not valid, please use the format --dpdk-cpu=<amount>m"
           exit 1
        fi
      ;;
      --dpdk-memory=*)
        DPDK_MEMORY="${1#*=}"
        if [[ $DPDK_MEMORY =~ ^[0-9]+(Gi)$ ]]
        then
           echo "MEMORY $DPDK_MEMORY"
        else
           echo "$DPDK_MEMORY is not valid, please use the format --dpdk-memory=<amount>Gi"
           exit 1
        fi
      ;;
      -?*)
        echo "Unknown argument $1"
        exit 1
      ;;
      *) break
    esac
    shift
  done
  set -u
fi

echo "-------------------------------"
echo "Kube-OVN Version:     $VERSION"
echo "Default Network Mode: $NETWORK_TYPE"
if [[ $NETWORK_TYPE = "vlan" ]];then
  echo "Default Vlan Nic:     $VLAN_INTERFACE_NAME"
  echo "Default Vlan ID:      $VLAN_ID"
fi
echo "Default Subnet CIDR:  $POD_CIDR"
echo "Join Subnet CIDR:     $JOIN_CIDR"
echo "Enable SVC LB:        $ENABLE_LB"
echo "Enable Networkpolicy: $ENABLE_NP"
echo "Enable EIP and SNAT:  $ENABLE_EIP_SNAT"
echo "Enable Mirror:        $ENABLE_MIRROR"
echo "-------------------------------"

if [[ $ENABLE_SSL = "true" ]];then
  echo "[Step 0/6] Generate SSL key and cert"
  exist=$(kubectl get secret -n kube-system kube-ovn-tls --ignore-not-found)
  if [[ $exist == "" ]];then
    docker run --rm -v "$PWD":/etc/ovn $REGISTRY/kube-ovn:$VERSION bash generate-ssl.sh
    kubectl create secret generic -n kube-system kube-ovn-tls --from-file=cacert=cacert.pem --from-file=cert=ovn-cert.pem --from-file=key=ovn-privkey.pem
    rm -rf cacert.pem ovn-cert.pem ovn-privkey.pem ovn-req.pem
  fi
  echo "-------------------------------"
  echo ""
fi

echo "[Step 1/6] Label kube-ovn-master node and label datapath type"
count=$(kubectl get no -l$LABEL --no-headers | wc -l)
node_label="$LABEL"
if [ "${count}" -eq 0 ]; then
  count=$(kubectl get no -l$DEPRECATED_LABEL --no-headers | wc -l)
  node_label="$DEPRECATED_LABEL"
  if [ "${count}" -eq 0 ]; then
    echo "ERROR: No node with label $LABEL or $DEPRECATED_LABEL found"
    exit 1
  fi
fi
kubectl label no -l$node_label kube-ovn/role=master --overwrite

if [ "$DPDK" = "true" ] || [ "$HYBRID_DPDK" = "true" ]; then
  kubectl label no -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel --overwrite
fi

echo "-------------------------------"
echo ""

echo "[Step 2/6] Install OVN components"
addresses=$(kubectl get no -lkube-ovn/role=master --no-headers -o wide | awk '{print $6}' | tr \\n ',' | sed 's/,$//')
count=$(kubectl get no -lkube-ovn/role=master --no-headers | wc -l)
echo "Install OVN DB in $addresses"

cat <<EOF > kube-ovn-crd.yaml
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: vpc-dnses.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: vpc-dnses
    singular: vpc-dns
    shortNames:
      - vpc-dns
    kind: VpcDns
    listKind: VpcDnsList
  scope: Cluster
  versions:
    - additionalPrinterColumns:
        - jsonPath: .status.active
          name: Active
          type: boolean
        - jsonPath: .spec.vpc
          name: Vpc
          type: string
        - jsonPath: .spec.subnet
          name: Subnet
          type: string
      name: v1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                vpc:
                  type: string
                subnet:
                  type: string
                replicas:
                  type: integer
                  minimum: 1
                  maximum: 3
            status:
              type: object
              properties:
                active:
                  type: boolean
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: switch-lb-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: switch-lb-rules
    singular: switch-lb-rule
    shortNames:
      - slr
    kind: SwitchLBRule
    listKind: SwitchLBRuleList
  scope: Cluster
  versions:
    - additionalPrinterColumns:
        - jsonPath: .spec.vip
          name: vip
          type: string
        - jsonPath: .status.ports
          name: port(s)
          type: string
        - jsonPath: .status.service
          name: service
          type: string
        - jsonPath: .metadata.creationTimestamp
          name: age
          type: date
      name: v1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                namespace:
                  type: string
                vip:
                  type: string
                sessionAffinity:
                  type: string
                ports:
                  items:
                    properties:
                      name:
                        type: string
                      port:
                        type: integer
                        minimum: 1
                        maximum: 65535
                      protocol:
                        type: string
                      targetPort:
                        type: integer
                        minimum: 1
                        maximum: 65535
                    type: object
                  type: array
                selector:
                  items:
                    type: string
                  type: array
                endpoints:
                  items:
                    type: string
                  type: array
            status:
              type: object
              properties:
                ports:
                  type: string
                service:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: vpc-nat-gateways.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: vpc-nat-gateways
    singular: vpc-nat-gateway
    shortNames:
      - vpc-nat-gw
    kind: VpcNatGateway
    listKind: VpcNatGatewayList
  scope: Cluster
  versions:
    - additionalPrinterColumns:
        - jsonPath: .spec.vpc
          name: Vpc
          type: string
        - jsonPath: .spec.subnet
          name: Subnet
          type: string
        - jsonPath: .spec.lanIp
          name: LanIP
          type: string
      name: v1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                externalSubnets:
                  items:
                    type: string
                  type: array
                selector:
                  type: array
                  items:
                    type: string
                qosPolicy:
                  type: string
                tolerations:
                  type: array
                  items:
                    type: object
                    properties:
                      key:
                        type: string
                      operator:
                        type: string
                        enum:
                          - Equal
                          - Exists
                      value:
                        type: string
                      effect:
                        type: string
                        enum:
                          - NoExecute
                          - NoSchedule
                          - PreferNoSchedule
                      tolerationSeconds:
                        type: integer
                affinity:
                  properties:
                    nodeAffinity:
                      properties:
                        preferredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              preference:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchFields:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                type: object
                              weight:
                                format: int32
                                type: integer
                            required:
                              - preference
                              - weight
                            type: object
                          type: array
                        requiredDuringSchedulingIgnoredDuringExecution:
                          properties:
                            nodeSelectorTerms:
                              items:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchFields:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                type: object
                              type: array
                          required:
                            - nodeSelectorTerms
                          type: object
                      type: object
                    podAffinity:
                      properties:
                        preferredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              podAffinityTerm:
                                properties:
                                  labelSelector:
                                    properties:
                                      matchExpressions:
                                        items:
                                          properties:
                                            key:
                                              type: string
                                              x-kubernetes-patch-strategy: merge
                                              x-kubernetes-patch-merge-key: key
                                            operator:
                                              type: string
                                            values:
                                              items:
                                                type: string
                                              type: array
                                          required:
                                            - key
                                            - operator
                                          type: object
                                        type: array
                                      matchLabels:
                                        additionalProperties:
                                          type: string
                                        type: object
                                    type: object
                                  namespaces:
                                    items:
                                      type: string
                                    type: array
                                  topologyKey:
                                    type: string
                                required:
                                  - topologyKey
                                type: object
                              weight:
                                format: int32
                                type: integer
                            required:
                              - podAffinityTerm
                              - weight
                            type: object
                          type: array
                        requiredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              labelSelector:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                          x-kubernetes-patch-strategy: merge
                                          x-kubernetes-patch-merge-key: key
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchLabels:
                                    additionalProperties:
                                      type: string
                                    type: object
                                type: object
                              namespaces:
                                items:
                                  type: string
                                type: array
                              topologyKey:
                                type: string
                            required:
                              - topologyKey
                            type: object
                          type: array
                      type: object
                    podAntiAffinity:
                      properties:
                        preferredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              podAffinityTerm:
                                properties:
                                  labelSelector:
                                    properties:
                                      matchExpressions:
                                        items:
                                          properties:
                                            key:
                                              type: string
                                              x-kubernetes-patch-strategy: merge
                                              x-kubernetes-patch-merge-key: key
                                            operator:
                                              type: string
                                            values:
                                              items:
                                                type: string
                                              type: array
                                          required:
                                            - key
                                            - operator
                                          type: object
                                        type: array
                                      matchLabels:
                                        additionalProperties:
                                          type: string
                                        type: object
                                    type: object
                                  namespaces:
                                    items:
                                      type: string
                                    type: array
                                  topologyKey:
                                    type: string
                                required:
                                  - topologyKey
                                type: object
                              weight:
                                format: int32
                                type: integer
                            required:
                              - podAffinityTerm
                              - weight
                            type: object
                          type: array
                        requiredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              labelSelector:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                          x-kubernetes-patch-strategy: merge
                                          x-kubernetes-patch-merge-key: key
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchLabels:
                                    additionalProperties:
                                      type: string
                                    type: object
                                type: object
                              namespaces:
                                items:
                                  type: string
                                type: array
                              topologyKey:
                                type: string
                            required:
                              - topologyKey
                            type: object
                          type: array
                      type: object
                  type: object
            spec:
              type: object
              properties:
                lanIp:
                  type: string
                subnet:
                  type: string
                externalSubnets:
                  items:
                    type: string
                  type: array
                vpc:
                  type: string
                selector:
                  type: array
                  items:
                    type: string
                qosPolicy:
                  type: string
                tolerations:
                  type: array
                  items:
                    type: object
                    properties:
                      key:
                        type: string
                      operator:
                        type: string
                        enum:
                          - Equal
                          - Exists
                      value:
                        type: string
                      effect:
                        type: string
                        enum:
                          - NoExecute
                          - NoSchedule
                          - PreferNoSchedule
                      tolerationSeconds:
                        type: integer
                affinity:
                  properties:
                    nodeAffinity:
                      properties:
                        preferredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              preference:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchFields:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                type: object
                              weight:
                                format: int32
                                type: integer
                            required:
                              - preference
                              - weight
                            type: object
                          type: array
                        requiredDuringSchedulingIgnoredDuringExecution:
                          properties:
                            nodeSelectorTerms:
                              items:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchFields:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                type: object
                              type: array
                          required:
                            - nodeSelectorTerms
                          type: object
                      type: object
                    podAffinity:
                      properties:
                        preferredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              podAffinityTerm:
                                properties:
                                  labelSelector:
                                    properties:
                                      matchExpressions:
                                        items:
                                          properties:
                                            key:
                                              type: string
                                              x-kubernetes-patch-strategy: merge
                                              x-kubernetes-patch-merge-key: key
                                            operator:
                                              type: string
                                            values:
                                              items:
                                                type: string
                                              type: array
                                          required:
                                            - key
                                            - operator
                                          type: object
                                        type: array
                                      matchLabels:
                                        additionalProperties:
                                          type: string
                                        type: object
                                    type: object
                                  namespaces:
                                    items:
                                      type: string
                                    type: array
                                  topologyKey:
                                    type: string
                                required:
                                  - topologyKey
                                type: object
                              weight:
                                format: int32
                                type: integer
                            required:
                              - podAffinityTerm
                              - weight
                            type: object
                          type: array
                        requiredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              labelSelector:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                          x-kubernetes-patch-strategy: merge
                                          x-kubernetes-patch-merge-key: key
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchLabels:
                                    additionalProperties:
                                      type: string
                                    type: object
                                type: object
                              namespaces:
                                items:
                                  type: string
                                type: array
                              topologyKey:
                                type: string
                            required:
                              - topologyKey
                            type: object
                          type: array
                      type: object
                    podAntiAffinity:
                      properties:
                        preferredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              podAffinityTerm:
                                properties:
                                  labelSelector:
                                    properties:
                                      matchExpressions:
                                        items:
                                          properties:
                                            key:
                                              type: string
                                              x-kubernetes-patch-strategy: merge
                                              x-kubernetes-patch-merge-key: key
                                            operator:
                                              type: string
                                            values:
                                              items:
                                                type: string
                                              type: array
                                          required:
                                            - key
                                            - operator
                                          type: object
                                        type: array
                                      matchLabels:
                                        additionalProperties:
                                          type: string
                                        type: object
                                    type: object
                                  namespaces:
                                    items:
                                      type: string
                                    type: array
                                  topologyKey:
                                    type: string
                                required:
                                  - topologyKey
                                type: object
                              weight:
                                format: int32
                                type: integer
                            required:
                              - podAffinityTerm
                              - weight
                            type: object
                          type: array
                        requiredDuringSchedulingIgnoredDuringExecution:
                          items:
                            properties:
                              labelSelector:
                                properties:
                                  matchExpressions:
                                    items:
                                      properties:
                                        key:
                                          type: string
                                          x-kubernetes-patch-strategy: merge
                                          x-kubernetes-patch-merge-key: key
                                        operator:
                                          type: string
                                        values:
                                          items:
                                            type: string
                                          type: array
                                      required:
                                        - key
                                        - operator
                                      type: object
                                    type: array
                                  matchLabels:
                                    additionalProperties:
                                      type: string
                                    type: object
                                type: object
                              namespaces:
                                items:
                                  type: string
                                type: array
                              topologyKey:
                                type: string
                            required:
                              - topologyKey
                            type: object
                          type: array
                      type: object
                  type: object
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: iptables-eips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: iptables-eips
    singular: iptables-eip
    shortNames:
      - eip
    kind: IptablesEIP
    listKind: IptablesEIPList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .status.ip
        name: IP
        type: string
      - jsonPath: .spec.macAddress
        name: Mac
        type: string
      - jsonPath: .status.nat
        name: Nat
        type: string
      - jsonPath: .spec.natGwDp
        name: NatGwDp
        type: string
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                ready:
                  type: boolean
                ip:
                  type: string
                nat:
                  type: string
                redo:
                  type: string
                qosPolicy:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                v4ip:
                  type: string
                v6ip:
                  type: string
                macAddress:
                  type: string
                natGwDp:
                  type: string
                qosPolicy:
                  type: string
                externalSubnet:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: iptables-fip-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: iptables-fip-rules
    singular: iptables-fip-rule
    shortNames:
      - fip
    kind: IptablesFIPRule
    listKind: IptablesFIPRuleList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .spec.eip
        name: Eip
        type: string
      - jsonPath: .status.v4ip
        name: V4ip
        type: string
      - jsonPath: .spec.internalIp
        name: InternalIp
        type: string
      - jsonPath: .status.v6ip
        name: V6ip
        type: string
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      - jsonPath: .status.natGwDp
        name: NatGwDp
        type: string
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                ready:
                  type: boolean
                v4ip:
                  type: string
                v6ip:
                  type: string
                natGwDp:
                  type: string
                redo:
                  type: string
                internalIp:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                eip:
                  type: string
                internalIp:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: iptables-dnat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: iptables-dnat-rules
    singular: iptables-dnat-rule
    shortNames:
      - dnat
    kind: IptablesDnatRule
    listKind: IptablesDnatRuleList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .spec.eip
        name: Eip
        type: string
      - jsonPath: .spec.protocol
        name: Protocol
        type: string
      - jsonPath: .status.v4ip
        name: V4ip
        type: string
      - jsonPath: .status.v6ip
        name: V6ip
        type: string
      - jsonPath: .spec.internalIp
        name: InternalIp
        type: string
      - jsonPath: .spec.externalPort
        name: ExternalPort
        type: string
      - jsonPath: .spec.internalPort
        name: InternalPort
        type: string
      - jsonPath: .status.natGwDp
        name: NatGwDp
        type: string
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                ready:
                  type: boolean
                v4ip:
                  type: string
                v6ip:
                  type: string
                natGwDp:
                  type: string
                redo:
                  type: string
                protocol:
                  type: string
                internalIp:
                  type: string
                internalPort:
                  type: string
                externalPort:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                eip:
                  type: string
                externalPort:
                  type: string
                protocol:
                  type: string
                internalIp:
                  type: string
                internalPort:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: iptables-snat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: iptables-snat-rules
    singular: iptables-snat-rule
    shortNames:
      - snat
    kind: IptablesSnatRule
    listKind: IptablesSnatRuleList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .spec.eip
        name: EIP
        type: string
      - jsonPath: .status.v4ip
        name: V4ip
        type: string
      - jsonPath: .status.v6ip
        name: V6ip
        type: string
      - jsonPath: .spec.internalCIDR
        name: InternalCIDR
        type: string
      - jsonPath: .status.natGwDp
        name: NatGwDp
        type: string
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                ready:
                  type: boolean
                v4ip:
                  type: string
                v6ip:
                  type: string
                natGwDp:
                  type: string
                redo:
                  type: string
                internalCIDR:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                eip:
                  type: string
                internalCIDR:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ovn-eips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: ovn-eips
    singular: ovn-eip
    shortNames:
      - oeip
    kind: OvnEip
    listKind: OvnEipList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .status.v4Ip
        name: V4IP
        type: string
      - jsonPath: .status.v6Ip
        name: V6IP
        type: string
      - jsonPath: .status.macAddress
        name: Mac
        type: string
      - jsonPath: .status.type
        name: Type
        type: string
      - jsonPath: .status.nat
        name: Nat
        type: string
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                type:
                  type: string
                nat:
                  type: string
                ready:
                  type: boolean
                v4Ip:
                  type: string
                v6Ip:
                  type: string
                macAddress:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                externalSubnet:
                  type: string
                type:
                  type: string
                v4Ip:
                  type: string
                v6Ip:
                  type: string
                macAddress:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ovn-fips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: ovn-fips
    singular: ovn-fip
    shortNames:
      - ofip
    kind: OvnFip
    listKind: OvnFipList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .status.vpc
        name: Vpc
        type: string
      - jsonPath: .status.v4Eip
        name: V4Eip
        type: string
      - jsonPath: .status.v4Ip
        name: V4Ip
        type: string
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      - jsonPath: .spec.ipType
        name: IpType
        type: string
      - jsonPath: .spec.ipName
        name: IpName
        type: string
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                ready:
                  type: boolean
                v4Eip:
                  type: string
                v4Ip:
                  type: string
                vpc:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                ovnEip:
                  type: string
                ipType:
                  type: string
                ipName:
                  type: string
                vpc:
                  type: string
                v4Ip:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ovn-snat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: ovn-snat-rules
    singular: ovn-snat-rule
    shortNames:
      - osnat
    kind: OvnSnatRule
    listKind: OvnSnatRuleList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .status.vpc
        name: Vpc
        type: string
      - jsonPath: .status.v4Eip
        name: V4Eip
        type: string
      - jsonPath: .status.v4IpCidr
        name: V4IpCidr
        type: string
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                ready:
                  type: boolean
                v4Eip:
                  type: string
                v4IpCidr:
                  type: string
                vpc:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                ovnEip:
                  type: string
                vpcSubnet:
                  type: string
                ipName:
                  type: string
                vpc:
                  type: string
                v4IpCidr:
                  type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ovn-dnat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: ovn-dnat-rules
    singular: ovn-dnat-rule
    shortNames:
      - odnat
    kind: OvnDnatRule
    listKind: OvnDnatRuleList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
        - jsonPath: .status.vpc
          name: Vpc
          type: string
        - jsonPath: .spec.ovnEip
          name: Eip
          type: string
        - jsonPath: .status.protocol
          name: Protocol
          type: string
        - jsonPath: .status.v4Eip
          name: V4Eip
          type: string
        - jsonPath: .status.v4Ip
          name: V4Ip
          type: string
        - jsonPath: .status.internalPort
          name: InternalPort
          type: string
        - jsonPath: .status.externalPort
          name: ExternalPort
          type: string
        - jsonPath: .spec.ipName
          name: IpName
          type: string
        - jsonPath: .status.ready
          name: Ready
          type: boolean
      schema:
          openAPIV3Schema:
            type: object
            properties:
              status:
                type: object
                properties:
                  ready:
                    type: boolean
                  v4Eip:
                    type: string
                  v4Ip:
                    type: string
                  vpc:
                    type: string
                  externalPort:
                    type: string
                  internalPort:
                    type: string
                  protocol:
                    type: string
                  ipName:
                    type: string
                  conditions:
                    type: array
                    items:
                      type: object
                      properties:
                        type:
                          type: string
                        status:
                          type: string
                        reason:
                          type: string
                        message:
                          type: string
                        lastUpdateTime:
                          type: string
                        lastTransitionTime:
                          type: string
              spec:
                type: object
                properties:
                  ovnEip:
                    type: string
                  ipType:
                    type: string
                  ipName:
                    type: string
                  externalPort:
                    type: string
                  internalPort:
                    type: string
                  protocol:
                    type: string
                  vpc:
                    type: string
                  v4Ip:
                    type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: vpcs.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - additionalPrinterColumns:
        - jsonPath: .status.enableExternal
          name: EnableExternal
          type: boolean
        - jsonPath: .status.enableBfd
          name: EnableBfd
          type: boolean
        - jsonPath: .status.standby
          name: Standby
          type: boolean
        - jsonPath: .status.subnets
          name: Subnets
          type: string
        - jsonPath: .status.extraExternalSubnets
          name: ExtraExternalSubnets
          type: string
        - jsonPath: .spec.namespaces
          name: Namespaces
          type: string
      name: v1
      schema:
        openAPIV3Schema:
          properties:
            spec:
              properties:
                enableExternal:
                  type: boolean
                enableBfd:
                  type: boolean
                namespaces:
                  items:
                    type: string
                  type: array
                extraExternalSubnets:
                  items:
                    type: string
                  type: array
                staticRoutes:
                  items:
                    properties:
                      policy:
                        type: string
                      cidr:
                        type: string
                      nextHopIP:
                        type: string
                      ecmpMode:
                        type: string
                      bfdId:
                        type: string
                      routeTable:
                        type: string
                    type: object
                  type: array
                policyRoutes:
                  items:
                    properties:
                      priority:
                        type: integer
                      action:
                        type: string
                      match:
                        type: string
                      nextHopIP:
                        type: string
                    type: object
                  type: array
                vpcPeerings:
                  items:
                    properties:
                      remoteVpc:
                        type: string
                      localConnectIP:
                        type: string
                    type: object
                  type: array
              type: object
            status:
              properties:
                conditions:
                  items:
                    properties:
                      lastTransitionTime:
                        type: string
                      lastUpdateTime:
                        type: string
                      message:
                        type: string
                      reason:
                        type: string
                      status:
                        type: string
                      type:
                        type: string
                    type: object
                  type: array
                default:
                  type: boolean
                defaultLogicalSwitch:
                  type: string
                router:
                  type: string
                standby:
                  type: boolean
                enableExternal:
                  type: boolean
                enableBfd:
                  type: boolean
                subnets:
                  items:
                    type: string
                  type: array
                extraExternalSubnets:
                  items:
                    type: string
                  type: array
                vpcPeerings:
                  items:
                    type: string
                  type: array
                tcpLoadBalancer:
                  type: string
                tcpSessionLoadBalancer:
                  type: string
                udpLoadBalancer:
                  type: string
                udpSessionLoadBalancer:
                  type: string
                sctpLoadBalancer:
                  type: string
                sctpSessionLoadBalancer:
                  type: string
              type: object
          type: object
      served: true
      storage: true
      subresources:
        status: {}
  names:
    kind: Vpc
    listKind: VpcList
    plural: vpcs
    shortNames:
      - vpc
    singular: vpc
  scope: Cluster
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ips.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      additionalPrinterColumns:
      - name: V4IP
        type: string
        jsonPath: .spec.v4IpAddress
      - name: V6IP
        type: string
        jsonPath: .spec.v6IpAddress
      - name: Mac
        type: string
        jsonPath: .spec.macAddress
      - name: Node
        type: string
        jsonPath: .spec.nodeName
      - name: Subnet
        type: string
        jsonPath: .spec.subnet
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                podName:
                  type: string
                namespace:
                  type: string
                subnet:
                  type: string
                attachSubnets:
                  type: array
                  items:
                    type: string
                nodeName:
                  type: string
                ipAddress:
                  type: string
                v4IpAddress:
                  type: string
                v6IpAddress:
                  type: string
                attachIps:
                  type: array
                  items:
                    type: string
                macAddress:
                  type: string
                attachMacs:
                  type: array
                  items:
                    type: string
                containerID:
                  type: string
                podType:
                  type: string
  scope: Cluster
  names:
    plural: ips
    singular: ip
    kind: IP
    shortNames:
      - ip
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: vips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: vips
    singular: vip
    shortNames:
      - vip
    kind: Vip
    listKind: VipList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      additionalPrinterColumns:
      - name: V4IP
        type: string
        jsonPath: .status.v4ip
      - name: V6IP
        type: string
        jsonPath: .status.v6ip
      - name: Mac
        type: string
        jsonPath: .status.mac
      - name: PMac
        type: string
        jsonPath: .spec.parentMac
      - name: Subnet
        type: string
        jsonPath: .spec.subnet
      - jsonPath: .status.ready
        name: Ready
        type: boolean
      - jsonPath: .status.type
        name: Type
        type: string
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                type:
                  type: string
                ready:
                  type: boolean
                v4ip:
                  type: string
                v6ip:
                  type: string
                mac:
                  type: string
                pv4ip:
                  type: string
                pv6ip:
                  type: string
                pmac:
                  type: string
                selector:
                  type: array
                  items:
                    type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                namespace:
                  type: string
                subnet:
                  type: string
                type:
                  type: string
                attachSubnets:
                  type: array
                  items:
                    type: string
                v4ip:
                  type: string
                macAddress:
                  type: string
                v6ip:
                  type: string
                parentV4ip:
                  type: string
                parentMac:
                  type: string
                parentV6ip:
                  type: string
                selector:
                  type: array
                  items:
                    type: string
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: subnets.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - name: Provider
        type: string
        jsonPath: .spec.provider
      - name: Vpc
        type: string
        jsonPath: .spec.vpc
      - name: Protocol
        type: string
        jsonPath: .spec.protocol
      - name: CIDR
        type: string
        jsonPath: .spec.cidrBlock
      - name: Private
        type: boolean
        jsonPath: .spec.private
      - name: NAT
        type: boolean
        jsonPath: .spec.natOutgoing
      - name: Default
        type: boolean
        jsonPath: .spec.default
      - name: GatewayType
        type: string
        jsonPath: .spec.gatewayType
      - name: V4Used
        type: number
        jsonPath: .status.v4usingIPs
      - name: V4Available
        type: number
        jsonPath: .status.v4availableIPs
      - name: V6Used
        type: number
        jsonPath: .status.v6usingIPs
      - name: V6Available
        type: number
        jsonPath: .status.v6availableIPs
      - name: ExcludeIPs
        type: string
        jsonPath: .spec.excludeIps
      - name: U2OInterconnectionIP
        type: string
        jsonPath: .status.u2oInterconnectionIP
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                v4availableIPs:
                  type: number
                v4usingIPs:
                  type: number
                v6availableIPs:
                  type: number
                v6usingIPs:
                  type: number
                activateGateway:
                  type: string
                dhcpV4OptionsUUID:
                  type: string
                dhcpV6OptionsUUID:
                  type: string
                u2oInterconnectionIP:
                  type: string
                u2oInterconnectionVPC:
                  type: string
                v4usingIPrange:
                  type: string
                v4availableIPrange:
                  type: string
                v6usingIPrange:
                  type: string
                v6availableIPrange:
                  type: string
                natOutgoingPolicyRules:
                  type: array
                  items:
                    type: object
                    properties:
                      ruleID:
                        type: string
                      action:
                        type: string
                        enum:
                          - nat
                          - forward
                      match:
                        type: object
                        properties:
                          srcIPs:
                            type: string
                          dstIPs:
                            type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                vpc:
                  type: string
                default:
                  type: boolean
                protocol:
                  type: string
                  enum:
                    - IPv4
                    - IPv6
                    - Dual
                cidrBlock:
                  type: string
                namespaces:
                  type: array
                  items:
                    type: string
                gateway:
                  type: string
                provider:
                  type: string
                excludeIps:
                  type: array
                  items:
                    type: string
                vips:
                  type: array
                  items:
                    type: string
                gatewayType:
                  type: string
                allowSubnets:
                  type: array
                  items:
                    type: string
                gatewayNode:
                  type: string
                natOutgoing:
                  type: boolean
                externalEgressGateway:
                  type: string
                policyRoutingPriority:
                  type: integer
                  minimum: 1
                  maximum: 32765
                policyRoutingTableID:
                  type: integer
                  minimum: 1
                  maximum: 2147483647
                  not:
                    enum:
                      - 252 # compat
                      - 253 # default
                      - 254 # main
                      - 255 # local
                mtu:
                  type: integer
                  minimum: 68
                  maximum: 65535
                private:
                  type: boolean
                vlan:
                  type: string
                logicalGateway:
                  type: boolean
                disableGatewayCheck:
                  type: boolean
                disableInterConnection:
                  type: boolean
                enableDHCP:
                  type: boolean
                dhcpV4Options:
                  type: string
                dhcpV6Options:
                  type: string
                enableIPv6RA:
                  type: boolean
                ipv6RAConfigs:
                  type: string
                allowEWTraffic:
                  type: boolean
                acls:
                  type: array
                  items:
                    type: object
                    properties:
                      direction:
                        type: string
                        enum:
                          - from-lport
                          - to-lport
                      priority:
                        type: integer
                        minimum: 0
                        maximum: 32767
                      match:
                        type: string
                      action:
                        type: string
                        enum:
                          - allow-related
                          - allow-stateless
                          - allow
                          - drop
                          - reject
                natOutgoingPolicyRules:
                  type: array
                  items:
                    type: object
                    properties:
                      action:
                        type: string
                        enum:
                          - nat
                          - forward
                      match:
                        type: object
                        properties:
                          srcIPs:
                            type: string
                          dstIPs:
                            type: string
                u2oInterconnection:
                  type: boolean
                u2oInterconnectionIP:
                  type: string
                enableLb:
                  type: boolean
                enableEcmp:
                  type: boolean
                enableMulticastSnoop:
                  type: boolean
                routeTable:
                  type: string
  scope: Cluster
  names:
    plural: subnets
    singular: subnet
    kind: Subnet
    shortNames:
      - subnet
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: ippools.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - name: Subnet
        type: string
        jsonPath: .spec.subnet
      - name: IPs
        type: string
        jsonPath: .spec.ips
      - name: V4Used
        type: number
        jsonPath: .status.v4UsingIPs
      - name: V4Available
        type: number
        jsonPath: .status.v4AvailableIPs
      - name: V6Used
        type: number
        jsonPath: .status.v6UsingIPs
      - name: V6Available
        type: number
        jsonPath: .status.v6AvailableIPs
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                subnet:
                  type: string
                  x-kubernetes-validations:
                    - rule: "self == oldSelf"
                      message: "This field is immutable."
                namespaces:
                  type: array
                  x-kubernetes-list-type: set
                  items:
                    type: string
                ips:
                  type: array
                  minItems: 1
                  x-kubernetes-list-type: set
                  items:
                    type: string
                    anyOf:
                      - format: ipv4
                      - format: ipv6
                      - format: cidr
                      - pattern: ^(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.\.(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])$
                      - pattern: ^((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))\.\.((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))$
              required:
                - subnet
                - ips
            status:
              type: object
              properties:
                v4AvailableIPs:
                  type: number
                v4UsingIPs:
                  type: number
                v6AvailableIPs:
                  type: number
                v6UsingIPs:
                  type: number
                v4AvailableIPRange:
                  type: string
                v4UsingIPRange:
                  type: string
                v6AvailableIPRange:
                  type: string
                v6UsingIPRange:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
  scope: Cluster
  names:
    plural: ippools
    singular: ippool
    kind: IPPool
    shortNames:
      - ippool
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: vlans.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                id:
                  type: integer
                  minimum: 0
                  maximum: 4095
                provider:
                  type: string
                vlanId:
                  type: integer
                  description: Deprecated in favor of id
                providerInterfaceName:
                  type: string
                  description: Deprecated in favor of provider
              required:
                - provider
            status:
              type: object
              properties:
                subnets:
                  type: array
                  items:
                    type: string
      additionalPrinterColumns:
      - name: ID
        type: string
        jsonPath: .spec.id
      - name: Provider
        type: string
        jsonPath: .spec.provider
  scope: Cluster
  names:
    plural: vlans
    singular: vlan
    kind: Vlan
    shortNames:
      - vlan
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: provider-networks.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      schema:
        openAPIV3Schema:
          type: object
          properties:
            metadata:
              type: object
              properties:
                name:
                  type: string
                  maxLength: 12
                  not:
                    enum:
                      - int
            spec:
              type: object
              properties:
                defaultInterface:
                  type: string
                  maxLength: 15
                  pattern: '^[^/\s]+$'
                customInterfaces:
                  type: array
                  items:
                    type: object
                    properties:
                      interface:
                        type: string
                        maxLength: 15
                        pattern: '^[^/\s]+$'
                      nodes:
                        type: array
                        items:
                          type: string
                exchangeLinkName:
                  type: boolean
                excludeNodes:
                  type: array
                  items:
                    type: string
              required:
                - defaultInterface
            status:
              type: object
              properties:
                ready:
                  type: boolean
                readyNodes:
                  type: array
                  items:
                    type: string
                notReadyNodes:
                  type: array
                  items:
                    type: string
                vlans:
                  type: array
                  items:
                    type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      node:
                        type: string
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
      additionalPrinterColumns:
      - name: DefaultInterface
        type: string
        jsonPath: .spec.defaultInterface
      - name: Ready
        type: boolean
        jsonPath: .status.ready
  scope: Cluster
  names:
    plural: provider-networks
    singular: provider-network
    kind: ProviderNetwork
    listKind: ProviderNetworkList
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: security-groups.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: security-groups
    singular: security-group
    shortNames:
      - sg
    kind: SecurityGroup
    listKind: SecurityGroupList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                ingressRules:
                  type: array
                  items:
                    type: object
                    properties:
                      ipVersion:
                        type: string
                      protocol:
                        type: string
                      priority:
                        type: integer
                      remoteType:
                        type: string
                      remoteAddress:
                        type: string
                      remoteSecurityGroup:
                        type: string
                      portRangeMin:
                        type: integer
                      portRangeMax:
                        type: integer
                      policy:
                        type: string
                egressRules:
                  type: array
                  items:
                    type: object
                    properties:
                      ipVersion:
                        type: string
                      protocol:
                        type: string
                      priority:
                        type: integer
                      remoteType:
                        type: string
                      remoteAddress:
                        type: string
                      remoteSecurityGroup:
                        type: string
                      portRangeMin:
                        type: integer
                      portRangeMax:
                        type: integer
                      policy:
                        type: string
                allowSameGroupTraffic:
                  type: boolean
            status:
              type: object
              properties:
                portGroup:
                  type: string
                allowSameGroupTraffic:
                  type: boolean
                ingressMd5:
                  type: string
                egressMd5:
                  type: string
                ingressLastSyncSuccess:
                  type: boolean
                egressLastSyncSuccess:
                  type: boolean
      subresources:
        status: {}
  conversion:
    strategy: None
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: qos-policies.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: qos-policies
    singular: qos-policy
    shortNames:
      - qos
    kind: QoSPolicy
    listKind: QoSPolicyList
  scope: Cluster
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - jsonPath: .spec.shared
        name: Shared
        type: string
      - jsonPath: .spec.bindingType
        name: BindingType
        type: string
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                shared:
                  type: boolean
                bindingType:
                  type: string
                bandwidthLimitRules:
                  type: array
                  items:
                    type: object
                    properties:
                      name:
                        type: string
                      interface:
                        type: string
                      rateMax:
                        type: string
                      burstMax:
                        type: string
                      priority:
                        type: integer
                      direction:
                        type: string
                      matchType:
                        type: string
                      matchValue:
                        type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                shared:
                  type: boolean
                bindingType:
                  type: string
                bandwidthLimitRules:
                  type: array
                  items:
                    type: object
                    properties:
                      name:
                        type: string
                      interface:
                        type: string
                      rateMax:
                        type: string
                      burstMax:
                        type: string
                      priority:
                        type: integer
                      direction:
                        type: string
                      matchType:
                        type: string
                      matchValue:
                        type: string
                    required:
                      - name
                  x-kubernetes-list-map-keys:
                    - name
                  x-kubernetes-list-type: map
EOF

cat <<EOF > ovn-ovs-sa.yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ovn-ovs
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    rbac.authorization.k8s.io/system-only: "true"
  name: system:ovn-ovs
rules:
  - apiGroups:
      - ""
    resources:
      - pods
    verbs:
      - get
      - patch
  - apiGroups:
      - ""
    resources:
      - services
      - endpoints
    verbs:
      - get
  - apiGroups:
      - apps
    resources:
      - controllerrevisions
    verbs:
      - get
      - list
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ovn-ovs
roleRef:
  name: system:ovn-ovs
  kind: ClusterRole
  apiGroup: rbac.authorization.k8s.io
subjects:
  - kind: ServiceAccount
    name: ovn-ovs
    namespace: kube-system
EOF

cat <<EOF > kube-ovn-sa.yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ovn
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    rbac.authorization.k8s.io/system-only: "true"
  name: system:ovn
rules:
  - apiGroups:
      - "kubeovn.io"
    resources:
      - vpcs
      - vpcs/status
      - vpc-nat-gateways
      - vpc-nat-gateways/status
      - subnets
      - subnets/status
      - ippools
      - ippools/status
      - ips
      - vips
      - vips/status
      - vlans
      - vlans/status
      - provider-networks
      - provider-networks/status
      - security-groups
      - security-groups/status
      - iptables-eips
      - iptables-fip-rules
      - iptables-dnat-rules
      - iptables-snat-rules
      - iptables-eips/status
      - iptables-fip-rules/status
      - iptables-dnat-rules/status
      - iptables-snat-rules/status
      - ovn-eips
      - ovn-fips
      - ovn-snat-rules
      - ovn-eips/status
      - ovn-fips/status
      - ovn-snat-rules/status
      - ovn-dnat-rules
      - ovn-dnat-rules/status
      - switch-lb-rules
      - switch-lb-rules/status
      - vpc-dnses
      - vpc-dnses/status
      - qos-policies
      - qos-policies/status
    verbs:
      - "*"
  - apiGroups:
      - ""
    resources:
      - pods
      - namespaces
    verbs:
      - get
      - list
      - patch
      - watch
  - apiGroups:
      - ""
    resources:
      - nodes
    verbs:
      - get
      - list
      - patch
      - update
      - watch
  - apiGroups:
      - ""
    resources:
      - pods/exec
    verbs:
      - create
  - apiGroups:
      - "k8s.cni.cncf.io"
    resources:
      - network-attachment-definitions
    verbs:
      - get
  - apiGroups:
      - ""
      - networking.k8s.io
    resources:
      - networkpolicies
      - configmaps
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - apps
    resources:
      - daemonsets
    verbs:
      - get
  - apiGroups:
      - ""
    resources:
      - services
      - services/status
    verbs:
      - get
      - list
      - update
      - create
      - delete
      - watch
  - apiGroups:
      - ""
    resources:
      - endpoints
    verbs:
      - create
      - update
      - get
      - list
      - watch
  - apiGroups:
      - apps
    resources:
      - statefulsets
      - deployments
      - deployments/scale
    verbs:
      - get
      - list
      - create
      - delete
      - update
  - apiGroups:
      - ""
    resources:
      - events
    verbs:
      - create
      - patch
      - update
  - apiGroups:
      - coordination.k8s.io
    resources:
      - leases
    verbs:
      - "*"
  - apiGroups:
      - "kubevirt.io"
    resources:
      - virtualmachines
      - virtualmachineinstances
    verbs:
      - get
      - list
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ovn
roleRef:
  name: system:ovn
  kind: ClusterRole
  apiGroup: rbac.authorization.k8s.io
subjects:
  - kind: ServiceAccount
    name: ovn
    namespace: kube-system
EOF

cat <<EOF > kube-ovn-cni-sa.yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-ovn-cni
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    rbac.authorization.k8s.io/system-only: "true"
  name: system:kube-ovn-cni
rules:
  - apiGroups:
      - "kubeovn.io"
      - ""
    resources:
      - subnets
      - provider-networks
      - pods
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - ""
      - "kubeovn.io"
    resources:
      - ovn-eips
      - ovn-eips/status
      - nodes
    verbs:
      - get
      - list
      - patch
      - watch
  - apiGroups:
      - "kubeovn.io"
    resources:
      - ips
    verbs:
      - get
      - update
  - apiGroups:
      - ""
    resources:
      - events
    verbs:
      - create
      - patch
      - update
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-ovn-cni
roleRef:
  name: system:kube-ovn-cni
  kind: ClusterRole
  apiGroup: rbac.authorization.k8s.io
subjects:
  - kind: ServiceAccount
    name: kube-ovn-cni
    namespace: kube-system
EOF

cat <<EOF > kube-ovn-app-sa.yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-ovn-app
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  annotations:
    rbac.authorization.k8s.io/system-only: "true"
  name: system:kube-ovn-app
rules:
  - apiGroups:
      - ""
    resources:
      - pods
      - nodes
    verbs:
      - get
      - list
  - apiGroups:
      - apps
    resources:
      - daemonsets
    verbs:
      - get
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: kube-ovn-app
roleRef:
  name: system:kube-ovn-app
  kind: ClusterRole
  apiGroup: rbac.authorization.k8s.io
subjects:
  - kind: ServiceAccount
    name: kube-ovn-app
    namespace: kube-system
EOF

kubectl apply -f kube-ovn-crd.yaml
kubectl apply -f ovn-ovs-sa.yaml
kubectl apply -f kube-ovn-sa.yaml
kubectl apply -f kube-ovn-cni-sa.yaml
kubectl apply -f kube-ovn-app-sa.yaml

cat <<EOF > ovn.yaml
---
kind: Service
apiVersion: v1
metadata:
  name: ovn-nb
  namespace: kube-system
spec:
  ports:
    - name: ovn-nb
      protocol: TCP
      port: 6641
      targetPort: 6641
  type: ClusterIP
  ${SVC_YAML_IPFAMILYPOLICY}
  selector:
    app: ovn-central
    ovn-nb-leader: "true"
  sessionAffinity: None

---
kind: Service
apiVersion: v1
metadata:
  name: ovn-sb
  namespace: kube-system
spec:
  ports:
    - name: ovn-sb
      protocol: TCP
      port: 6642
      targetPort: 6642
  type: ClusterIP
  ${SVC_YAML_IPFAMILYPOLICY}
  selector:
    app: ovn-central
    ovn-sb-leader: "true"
  sessionAffinity: None

---
kind: Service
apiVersion: v1
metadata:
  name: ovn-northd
  namespace: kube-system
spec:
  ports:
    - name: ovn-northd
      protocol: TCP
      port: 6643
      targetPort: 6643
  type: ClusterIP
  ${SVC_YAML_IPFAMILYPOLICY}
  selector:
    app: ovn-central
    ovn-northd-leader: "true"
  sessionAffinity: None
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: ovn-central
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      OVN components: northd, nb and sb.
spec:
  replicas: $count
  strategy:
    rollingUpdate:
      maxSurge: 0
      maxUnavailable: 1
    type: RollingUpdate
  selector:
    matchLabels:
      app: ovn-central
  template:
    metadata:
      labels:
        app: ovn-central
        component: network
        type: infra
    spec:
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - effect: NoExecute
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: ovn-central
              topologyKey: kubernetes.io/hostname
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn-ovs
      hostNetwork: true
      containers:
        - name: ovn-central
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: 
          - /kube-ovn/start-db.sh
          securityContext:
            capabilities:
              add: ["SYS_NICE"]
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: NODE_IPS
              value: $addresses
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: POD_IPS
              valueFrom:
                fieldRef:
                  fieldPath: status.podIPs
            - name: ENABLE_BIND_LOCAL_IP
              value: "$ENABLE_BIND_LOCAL_IP"
            - name: DEBUG_WRAPPER
              value: "$DEBUG_WRAPPER"
            - name: PROBE_INTERVAL
              value: "180000"
            - name: OVN_LEADER_PROBE_INTERVAL
              value: "5"
            - name: OVN_NORTHD_N_THREADS
              value: "1"
            - name: ENABLE_COMPACT
              value: "$ENABLE_COMPACT"
          resources:
            requests:
              cpu: 300m
              memory: 300Mi
            limits:
              cpu: 4
              memory: 4Gi
          volumeMounts:
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /sys
              name: host-sys
              readOnly: true
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /etc/ovn
              name: host-config-ovn
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
            - mountPath: /var/log/ovn
              name: host-log-ovn
            - mountPath: /etc/localtime
              name: localtime
              readOnly: true
            - mountPath: /var/run/tls
              name: kube-ovn-tls
          readinessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovn-healthcheck.sh
            periodSeconds: 15
            timeoutSeconds: 45
          livenessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovn-healthcheck.sh
            initialDelaySeconds: 30
            periodSeconds: 15
            failureThreshold: 5
            timeoutSeconds: 45
      nodeSelector:
        kubernetes.io/os: "linux"
        kube-ovn/role: "master"
      volumes:
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: host-sys
          hostPath:
            path: /sys
        - name: host-config-openvswitch
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-config-ovn
          hostPath:
            path: /etc/origin/ovn
        - name: host-log-ovs
          hostPath:
            path: $LOG_DIR/openvswitch
        - name: host-log-ovn
          hostPath:
            path: $LOG_DIR/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
EOF

kubectl apply -f ovn.yaml

if $DPDK; then
  cat <<EOF > ovs-ovn-ds.yaml
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: ovs-ovn
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      This daemon set launches the openvswitch daemon.
spec:
  selector:
    matchLabels:
      app: ovs-dpdk
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  template:
    metadata:
      labels:
        app: ovs-dpdk
        component: network
        type: infra
    spec:
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - effect: NoExecute
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
      priorityClassName: system-node-critical
      serviceAccountName: ovn-ovs
      hostNetwork: true
      hostPID: true
      containers:
        - name: openvswitch
          image: "$REGISTRY/kube-ovn-dpdk:$DPDK_VERSION-$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ovs-dpdk.sh"]
          securityContext:
            runAsUser: 0
            privileged: true
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: OVN_DB_IPS
              value: $addresses
            - name: OVN_REMOTE_PROBE_INTERVAL
              value: "10000" 
            - name: OVN_REMOTE_OPENFLOW_INTERVAL
              value: "180"
          volumeMounts:
            - mountPath: /var/run/netns
              name: host-ns
              mountPropagation: HostToContainer
            - mountPath: /lib/modules
              name: host-modules
              readOnly: true
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /sys
              name: host-sys
              readOnly: true
            - mountPath: /etc/cni/net.d
              name: cni-conf
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /etc/ovn
              name: host-config-ovn
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
            - mountPath: /var/log/ovn
              name: host-log-ovn
            - mountPath: /opt/ovs-config
              name: host-config-ovs
            - mountPath: /dev/hugepages
              name: hugepage
            - mountPath: /etc/localtime
              name: localtime
            - mountPath: /var/run/tls
              name: kube-ovn-tls
          readinessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovs-dpdk-healthcheck.sh
            periodSeconds: 5
            timeoutSeconds: 45
          livenessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovs-dpdk-healthcheck.sh
            initialDelaySeconds: 60
            periodSeconds: 5
            failureThreshold: 5
            timeoutSeconds: 45
          resources:
            requests:
              cpu: $DPDK_CPU
              memory: $DPDK_MEMORY
            limits:
              cpu: $DPDK_CPU
              memory: $DPDK_MEMORY
              hugepages-1Gi: 1Gi
      nodeSelector:
        kubernetes.io/os: "linux"
        ovn.kubernetes.io/ovs_dp_type: "kernel"
      volumes:
        - name: host-modules
          hostPath:
            path: /lib/modules
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: host-sys
          hostPath:
            path: /sys
        - name: host-ns
          hostPath:
            path: /var/run/netns
        - name: cni-conf
          hostPath:
            path: /etc/cni/net.d
        - name: host-config-openvswitch
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-config-ovn
          hostPath:
            path: /etc/origin/ovn
        - name: host-log-ovs
          hostPath:
            path: $LOG_DIR/openvswitch
        - name: host-log-ovn
          hostPath:
            path: $LOG_DIR/ovn
        - name: host-config-ovs
          hostPath:
            path: /opt/ovs-config
            type: DirectoryOrCreate
        - name: hugepage
          emptyDir:
            medium: HugePages
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
EOF

else
  cat <<EOF > ovs-ovn-ds.yaml
---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: ovs-ovn
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      This daemon set launches the openvswitch daemon.
spec:
  selector:
    matchLabels:
      app: ovs
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  template:
    metadata:
      labels:
        app: ovs
        component: network
        type: infra
    spec:
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - effect: NoExecute
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
      priorityClassName: system-node-critical
      serviceAccountName: ovn-ovs
      hostNetwork: true
      hostPID: true
      containers:
        - name: openvswitch
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: 
          - /kube-ovn/start-ovs.sh
          securityContext:
            runAsUser: 0
            privileged: true
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: HW_OFFLOAD
              value: "$HW_OFFLOAD"
            - name: TUNNEL_TYPE
              value: "$TUNNEL_TYPE"
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: OVN_DB_IPS
              value: $addresses
            - name: DEBUG_WRAPPER
              value: "$DEBUG_WRAPPER"
            - name: OVN_REMOTE_PROBE_INTERVAL
              value: "10000" 
            - name: OVN_REMOTE_OPENFLOW_INTERVAL
              value: "180"
          volumeMounts:
            - mountPath: /var/run/netns
              name: host-ns
              mountPropagation: HostToContainer
            - mountPath: /lib/modules
              name: host-modules
              readOnly: true
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /sys
              name: host-sys
              readOnly: true
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /etc/ovn
              name: host-config-ovn
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
            - mountPath: /var/log/ovn
              name: host-log-ovn
            - mountPath: /etc/localtime
              name: localtime
              readOnly: true
            - mountPath: /var/run/tls
              name: kube-ovn-tls
            - mountPath: /var/run/containerd
              name: cruntime
              readOnly: true
          readinessProbe:
            exec:
              command:
                - bash
                - -c
                - LOG_ROTATE=true /kube-ovn/ovs-healthcheck.sh
            initialDelaySeconds: 10
            periodSeconds: 5
            timeoutSeconds: 45
          livenessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovs-healthcheck.sh
            initialDelaySeconds: 60
            periodSeconds: 5
            failureThreshold: 5
            timeoutSeconds: 45
          resources:
            requests:
              cpu: 200m
              memory: 200Mi
            limits:
              cpu: "2"
              memory: 1000Mi
      nodeSelector:
        kubernetes.io/os: "linux"
      volumes:
        - name: host-modules
          hostPath:
            path: /lib/modules
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: host-sys
          hostPath:
            path: /sys
        - name: host-ns
          hostPath:
            path: /var/run/netns
        - name: host-config-openvswitch
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-config-ovn
          hostPath:
            path: /etc/origin/ovn
        - name: host-log-ovs
          hostPath:
            path: $LOG_DIR/openvswitch
        - name: host-log-ovn
          hostPath:
            path: $LOG_DIR/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - hostPath:
            path: /var/run/containerd
          name: cruntime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
EOF
fi

kubectl apply -f ovs-ovn-ds.yaml

if $HYBRID_DPDK; then

cat <<EOF > ovn-dpdk.yaml
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: ovs-ovn-dpdk
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      This daemon set launches the openvswitch daemon.
spec:
  selector:
    matchLabels:
      app: ovs-dpdk
  updateStrategy:
    type: OnDelete
  template:
    metadata:
      labels:
        app: ovs-dpdk
        component: network
        type: infra
    spec:
      tolerations:
      - operator: Exists
      priorityClassName: system-node-critical
      serviceAccountName: ovn-ovs
      hostNetwork: true
      hostPID: true
      containers:
        - name: openvswitch
          image: "$REGISTRY/kube-ovn:${VERSION}-dpdk"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ovs-dpdk-v2.sh"]
          securityContext:
            runAsUser: 0
            privileged: true
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: HW_OFFLOAD
              value: "$HW_OFFLOAD"
            - name: TUNNEL_TYPE
              value: "$TUNNEL_TYPE"
            - name: DPDK_TUNNEL_IFACE
              value: "$DPDK_TUNNEL_IFACE"
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: OVN_DB_IPS
              value: $addresses
            - name: OVN_REMOTE_PROBE_INTERVAL
              value: "10000" 
            - name: OVN_REMOTE_OPENFLOW_INTERVAL
              value: "180"
          volumeMounts:
            - mountPath: /opt/ovs-config
              name: host-config-ovs
            - name: shareddir
              mountPath: $KUBELET_DIR/pods
            - name: hugepage
              mountPath: /dev/hugepages
            - mountPath: /lib/modules
              name: host-modules
              readOnly: true
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
              mountPropagation: HostToContainer
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /sys
              name: host-sys
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /etc/ovn
              name: host-config-ovn
            - mountPath: $LOG_DIR/openvswitch
              name: host-log-ovs
            - mountPath: $LOG_DIR/ovn
              name: host-log-ovn
            - mountPath: /etc/localtime
              name: localtime
              readOnly: true
            - mountPath: /var/run/tls
              name: kube-ovn-tls
          readinessProbe:
            exec:
              command:
                - bash
                - -c
                - LOG_ROTATE=true /kube-ovn/ovs-healthcheck.sh
            periodSeconds: 5
            timeoutSeconds: 45
          livenessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovs-healthcheck.sh
            initialDelaySeconds: 60
            periodSeconds: 5
            failureThreshold: 5
            timeoutSeconds: 45
          resources:
            requests:
              cpu: 200m
              hugepages-2Mi: 1Gi
              memory: 200Mi
            limits:
              cpu: 1000m
              hugepages-2Mi: 1Gi
              memory: 800Mi
      nodeSelector:
        kubernetes.io/os: "linux"
        ovn.kubernetes.io/ovs_dp_type: "userspace"
      volumes:
        - name: host-config-ovs
          hostPath:
            path: /opt/ovs-config
            type: DirectoryOrCreate
        - name: shareddir
          hostPath:
            path: $KUBELET_DIR/pods
            type: ''
        - name: hugepage
          emptyDir:
            medium: HugePages
        - name: host-modules
          hostPath:
            path: /lib/modules
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: host-sys
          hostPath:
            path: /sys
        - name: host-config-openvswitch
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-config-ovn
          hostPath:
            path: /etc/origin/ovn
        - name: host-log-ovs
          hostPath:
            path: $LOG_DIR/openvswitch
        - name: host-log-ovn
          hostPath:
            path: $LOG_DIR/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
EOF
kubectl apply -f ovn-dpdk.yaml
fi
kubectl rollout status deployment/ovn-central -n kube-system --timeout 300s
kubectl rollout status daemonset/ovs-ovn -n kube-system --timeout 120s
echo "-------------------------------"
echo ""

echo "[Step 3/6] Install Kube-OVN"

cat <<EOF > kube-ovn.yaml
---
kind: ConfigMap
apiVersion: v1
metadata:
  name: ovn-vpc-nat-config
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      kube-ovn vpc-nat common config
data:
  image: $REGISTRY/$VPC_NAT_IMAGE:$VERSION
---
kind: ConfigMap
apiVersion: v1
metadata:
  name: ovn-vpc-nat-gw-config
  namespace: kube-system
data:
  enable-vpc-nat-gw: "$ENABLE_NAT_GW"
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: kube-ovn-controller
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      kube-ovn controller
spec:
  replicas: $count
  selector:
    matchLabels:
      app: kube-ovn-controller
  strategy:
    rollingUpdate:
      maxSurge: 0%
      maxUnavailable: 100%
    type: RollingUpdate
  template:
    metadata:
      labels:
        app: kube-ovn-controller
        component: network
        type: infra
    spec:
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
      affinity:
        nodeAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
          - preference:
              matchExpressions:
              - key: "ovn.kubernetes.io/ic-gw"
                operator: NotIn
                values:
                - "true"
            weight: 100
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: kube-ovn-controller
              topologyKey: kubernetes.io/hostname
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
      hostNetwork: true
      containers:
        - name: kube-ovn-controller
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          args:
          - /kube-ovn/start-controller.sh
          - --default-cidr=$POD_CIDR
          - --default-gateway=$POD_GATEWAY
          - --default-gateway-check=$CHECK_GATEWAY
          - --default-logical-gateway=$LOGICAL_GATEWAY
          - --default-u2o-interconnection=$U2O_INTERCONNECTION
          - --default-exclude-ips=$EXCLUDE_IPS
          - --node-switch-cidr=$JOIN_CIDR
          - --service-cluster-ip-range=$SVC_CIDR
          - --network-type=$NETWORK_TYPE
          - --default-interface-name=$VLAN_INTERFACE_NAME
          - --default-exchange-link-name=$EXCHANGE_LINK_NAME
          - --default-vlan-id=$VLAN_ID
          - --ls-dnat-mod-dl-dst=$LS_DNAT_MOD_DL_DST
          - --ls-ct-skip-dst-lport-ips=$LS_CT_SKIP_DST_LPORT_IPS
          - --pod-nic-type=$POD_NIC_TYPE
          - --enable-lb=$ENABLE_LB
          - --enable-np=$ENABLE_NP
          - --enable-eip-snat=$ENABLE_EIP_SNAT
          - --enable-external-vpc=$ENABLE_EXTERNAL_VPC
          - --logtostderr=false
          - --alsologtostderr=true
          - --gc-interval=$GC_INTERVAL
          - --inspect-interval=$INSPECT_INTERVAL
          - --log_file=/var/log/kube-ovn/kube-ovn-controller.log
          - --log_file_max_size=0
          - --enable-lb-svc=$ENABLE_LB_SVC
          - --keep-vm-ip=$ENABLE_KEEP_VM_IP
          - --node-local-dns-ip=$NODE_LOCAL_DNS_IP
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: KUBE_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: OVN_DB_IPS
              value: $addresses
            - name: POD_IPS
              valueFrom:
                fieldRef:
                  fieldPath: status.podIPs
            - name: ENABLE_BIND_LOCAL_IP
              value: "$ENABLE_BIND_LOCAL_IP"
          volumeMounts:
            - mountPath: /etc/localtime
              name: localtime
              readOnly: true
            - mountPath: /var/log/kube-ovn
              name: kube-ovn-log
            # ovn-ic log directory
            - mountPath: /var/log/ovn
              name: ovn-log
            - mountPath: /var/run/tls
              name: kube-ovn-tls
          readinessProbe:
            exec:
              command:
                - /kube-ovn/kube-ovn-controller-healthcheck
            periodSeconds: 3
            timeoutSeconds: 45
          livenessProbe:
            exec:
              command:
                - /kube-ovn/kube-ovn-controller-healthcheck
            initialDelaySeconds: 300
            periodSeconds: 7
            failureThreshold: 5
            timeoutSeconds: 45
          resources:
            requests:
              cpu: 200m
              memory: 200Mi
            limits:
              cpu: 1000m
              memory: 1Gi
      nodeSelector:
        kubernetes.io/os: "linux"
        kube-ovn/role: master
      volumes:
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-log
          hostPath:
            path: $LOG_DIR/kube-ovn
        - name: ovn-log
          hostPath:
            path: $LOG_DIR/ovn
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls

---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: kube-ovn-cni
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      This daemon set launches the kube-ovn cni daemon.
spec:
  selector:
    matchLabels:
      app: kube-ovn-cni
  template:
    metadata:
      labels:
        app: kube-ovn-cni
        component: network
        type: infra
    spec:
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - effect: NoExecute
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
      priorityClassName: system-node-critical
      serviceAccountName: kube-ovn-cni
      hostNetwork: true
      hostPID: true
      initContainers:
      - name: install-cni
        image: "$REGISTRY/kube-ovn:$VERSION"
        imagePullPolicy: $IMAGE_PULL_POLICY
        command: ["/kube-ovn/install-cni.sh"]
        securityContext:
          runAsUser: 0
          privileged: true
        volumeMounts:
          - mountPath: /opt/cni/bin
            name: cni-bin
          - mountPath: /usr/local/bin
            name: local-bin
      containers:
      - name: cni-server
        image: "$REGISTRY/kube-ovn:$VERSION"
        imagePullPolicy: $IMAGE_PULL_POLICY
        command:
          - bash
          - /kube-ovn/start-cniserver.sh
        args:
          - --enable-mirror=$ENABLE_MIRROR
          - --enable-arp-detect-ip-conflict=$ENABLE_ARP_DETECT_IP_CONFLICT
          - --encap-checksum=true
          - --service-cluster-ip-range=$SVC_CIDR
          - --iface=${IFACE}
          - --dpdk-tunnel-iface=${DPDK_TUNNEL_IFACE}
          - --network-type=$TUNNEL_TYPE
          - --default-interface-name=$VLAN_INTERFACE_NAME
          - --cni-conf-name=${CNI_CONFIG_PRIORITY}-kube-ovn.conflist
          - --logtostderr=false
          - --alsologtostderr=true
          - --log_file=/var/log/kube-ovn/kube-ovn-cni.log
          - --log_file_max_size=0
          - --kubelet-dir=$KUBELET_DIR
          - --enable-tproxy=$ENABLE_TPROXY
          - --ovs-vsctl-concurrency=$OVS_VSCTL_CONCURRENCY
        securityContext:
          runAsUser: 0
          privileged: true
        env:
          - name: ENABLE_SSL
            value: "$ENABLE_SSL"
          - name: POD_IP
            valueFrom:
              fieldRef:
                fieldPath: status.podIP
          - name: KUBE_NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
          - name: MODULES
            value: $MODULES
          - name: RPMS
            value: $RPMS
          - name: POD_IPS
            valueFrom:
              fieldRef:
                fieldPath: status.podIPs
          - name: ENABLE_BIND_LOCAL_IP
            value: "$ENABLE_BIND_LOCAL_IP"
          - name: DBUS_SYSTEM_BUS_ADDRESS
            value: "unix:path=/host/var/run/dbus/system_bus_socket"
        volumeMounts:
          - name: host-modules
            mountPath: /lib/modules
            readOnly: true
          - name: shared-dir
            mountPath: $KUBELET_DIR/pods
          - mountPath: /etc/openvswitch
            name: systemid
            readOnly: true
          - mountPath: /etc/cni/net.d
            name: cni-conf
          - mountPath: /run/openvswitch
            name: host-run-ovs
            mountPropagation: Bidirectional
          - mountPath: /run/ovn
            name: host-run-ovn
          - mountPath: /host/var/run/dbus
            name: host-dbus
            mountPropagation: HostToContainer
          - mountPath: /var/run/netns
            name: host-ns
            mountPropagation: Bidirectional
          - mountPath: /var/log/kube-ovn
            name: kube-ovn-log
          - mountPath: /var/log/openvswitch
            name: host-log-ovs
          - mountPath: /var/log/ovn
            name: host-log-ovn
          - mountPath: /etc/localtime
            name: localtime
            readOnly: true
          - mountPath: /tmp
            name: tmp
        livenessProbe:
          failureThreshold: 3
          initialDelaySeconds: 30
          periodSeconds: 7
          successThreshold: 1
          tcpSocket:
            port: 10665
          timeoutSeconds: 3
        readinessProbe:
          failureThreshold: 3
          periodSeconds: 7
          successThreshold: 1
          tcpSocket:
            port: 10665
          timeoutSeconds: 3
        resources:
          requests:
            cpu: 100m
            memory: 100Mi
          limits:
            cpu: 1000m
            memory: 1Gi
      nodeSelector:
        kubernetes.io/os: "linux"
      volumes:
        - name: host-modules
          hostPath:
            path: /lib/modules
        - name: shared-dir
          hostPath:
            path: $KUBELET_DIR/pods
        - name: systemid
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: cni-conf
          hostPath:
            path: $CNI_CONF_DIR
        - name: cni-bin
          hostPath:
            path: $CNI_BIN_DIR
        - name: host-ns
          hostPath:
            path: /var/run/netns
        - name: host-dbus
          hostPath:
            path: /var/run/dbus
        - name: host-log-ovs
          hostPath:
            path: $LOG_DIR/openvswitch
        - name: kube-ovn-log
          hostPath:
            path: $LOG_DIR/kube-ovn
        - name: host-log-ovn
          hostPath:
            path: $LOG_DIR/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: tmp
          hostPath:
            path: /tmp
        - name: local-bin
          hostPath:
            path: /usr/local/bin

---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: kube-ovn-pinger
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      This daemon set launches the pinger daemon.
spec:
  selector:
    matchLabels:
      app: kube-ovn-pinger
  updateStrategy:
    type: RollingUpdate
  template:
    metadata:
      labels:
        app: kube-ovn-pinger
        component: network
        type: infra
    spec:
      priorityClassName: system-node-critical
      serviceAccountName: kube-ovn-app
      hostPID: true
      containers:
        - name: pinger
          image: "$REGISTRY/kube-ovn:$VERSION"
          command:
          - /kube-ovn/kube-ovn-pinger
          args:
          - --external-address=$PINGER_EXTERNAL_ADDRESS
          - --external-dns=$PINGER_EXTERNAL_DOMAIN
          - --logtostderr=false
          - --alsologtostderr=true
          - --log_file=/var/log/kube-ovn/kube-ovn-pinger.log
          - --log_file_max_size=0
          imagePullPolicy: $IMAGE_PULL_POLICY
          securityContext:
            runAsUser: 0
            privileged: false
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: HOST_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.hostIP
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
          volumeMounts:
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
              readOnly: true
            - mountPath: /var/log/ovn
              name: host-log-ovn
              readOnly: true
            - mountPath: /var/log/kube-ovn
              name: kube-ovn-log
            - mountPath: /etc/localtime
              name: localtime
              readOnly: true
            - mountPath: /var/run/tls
              name: kube-ovn-tls
          resources:
            requests:
              cpu: 100m
              memory: 100Mi
            limits:
              cpu: 200m
              memory: 400Mi
      nodeSelector:
        kubernetes.io/os: "linux"
      volumes:
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: host-config-openvswitch
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-log-ovs
          hostPath:
            path: $LOG_DIR/openvswitch
        - name: kube-ovn-log
          hostPath:
            path: $LOG_DIR/kube-ovn
        - name: host-log-ovn
          hostPath:
            path: $LOG_DIR/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: kube-ovn-monitor
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      Metrics for OVN components: northd, nb and sb.
spec:
  replicas: 1
  strategy:
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 1
    type: RollingUpdate
  selector:
    matchLabels:
      app: kube-ovn-monitor
  template:
    metadata:
      labels:
        app: kube-ovn-monitor
        component: network
        type: infra
    spec:
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: kube-ovn-monitor
              topologyKey: kubernetes.io/hostname
      priorityClassName: system-cluster-critical
      serviceAccountName: kube-ovn-app
      hostNetwork: true
      containers:
        - name: kube-ovn-monitor
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ovn-monitor.sh"]
          args:
          - --log_file=/var/log/kube-ovn/kube-ovn-monitor.log
          - --logtostderr=false
          - --alsologtostderr=true
          - --log_file_max_size=0
          securityContext:
            runAsUser: 0
            privileged: false
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: POD_IPS
              valueFrom:
                fieldRef:
                  fieldPath: status.podIPs
            - name: ENABLE_BIND_LOCAL_IP
              value: "$ENABLE_BIND_LOCAL_IP"
          resources:
            requests:
              cpu: 200m
              memory: 200Mi
            limits:
              cpu: 200m
              memory: 200Mi
          volumeMounts:
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /etc/ovn
              name: host-config-ovn
            - mountPath: /var/log/ovn
              name: host-log-ovn
              readOnly: true
            - mountPath: /etc/localtime
              name: localtime
              readOnly: true
            - mountPath: /var/run/tls
              name: kube-ovn-tls
            - mountPath: /var/log/kube-ovn
              name: kube-ovn-log
          livenessProbe:
            failureThreshold: 3
            initialDelaySeconds: 30
            periodSeconds: 7
            successThreshold: 1
            tcpSocket:
              port: 10661
            timeoutSeconds: 3
          readinessProbe:
            failureThreshold: 3
            initialDelaySeconds: 30
            periodSeconds: 7
            successThreshold: 1
            tcpSocket:
              port: 10661
            timeoutSeconds: 3
      nodeSelector:
        kubernetes.io/os: "linux"
        kube-ovn/role: "master"
      volumes:
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: host-config-openvswitch
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-config-ovn
          hostPath:
            path: /etc/origin/ovn
        - name: host-log-ovn
          hostPath:
            path: $LOG_DIR/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
        - name: kube-ovn-log
          hostPath:
            path: $LOG_DIR/kube-ovn
---
kind: Service
apiVersion: v1
metadata:
  name: kube-ovn-monitor
  namespace: kube-system
  labels:
    app: kube-ovn-monitor
spec:
  ports:
    - name: metrics
      port: 10661
  type: ClusterIP
  ${SVC_YAML_IPFAMILYPOLICY}
  selector:
    app: kube-ovn-monitor
  sessionAffinity: None
---
kind: Service
apiVersion: v1
metadata:
  name: kube-ovn-pinger
  namespace: kube-system
  labels:
    app: kube-ovn-pinger
spec:
  ${SVC_YAML_IPFAMILYPOLICY}
  selector:
    app: kube-ovn-pinger
  ports:
    - port: 8080
      name: metrics
---
kind: Service
apiVersion: v1
metadata:
  name: kube-ovn-controller
  namespace: kube-system
  labels:
    app: kube-ovn-controller
spec:
  ${SVC_YAML_IPFAMILYPOLICY}
  selector:
    app: kube-ovn-controller
  ports:
    - port: 10660
      name: metrics
---
kind: Service
apiVersion: v1
metadata:
  name: kube-ovn-cni
  namespace: kube-system
  labels:
    app: kube-ovn-cni
spec:
  ${SVC_YAML_IPFAMILYPOLICY}
  selector:
    app: kube-ovn-cni
  ports:
    - port: 10665
      name: metrics
EOF

kubectl apply -f kube-ovn.yaml
kubectl rollout status deployment/kube-ovn-controller -n kube-system --timeout 300s
kubectl rollout status daemonset/kube-ovn-cni -n kube-system --timeout 300s

if $ENABLE_IC; then

cat <<EOF > ovn-ic-controller.yaml
kind: Deployment
apiVersion: apps/v1
metadata:
  name: ovn-ic-controller
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      OVN IC Controller
spec:
  replicas: 1
  strategy:
    rollingUpdate:
      maxSurge: 0
      maxUnavailable: 1
    type: RollingUpdate
  selector:
    matchLabels:
      app: ovn-ic-controller
  template:
    metadata:
      labels:
        app: ovn-ic-controller
        component: network
        type: infra
    spec:
      tolerations:
        - effect: NoSchedule
          operator: Exists
        - effect: NoExecute
          operator: Exists
        - key: CriticalAddonsOnly
          operator: Exists
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchLabels:
                  app: ovn-ic-controller
              topologyKey: kubernetes.io/hostname
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
      hostNetwork: true
      containers:
        - name: ovn-ic-controller
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ic-controller.sh"]
          args:
          - --log_file=/var/log/kube-ovn/kube-ovn-ic-controller.log
          - --log_file_max_size=0
          - --logtostderr=false
          - --alsologtostderr=true
          securityContext:
            capabilities:
              add: ["SYS_NICE"]
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: OVN_DB_IPS
              value: $addresses
          resources:
            requests:
              cpu: 300m
              memory: 200Mi
            limits:
              cpu: 3
              memory: 1Gi
          volumeMounts:
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /etc/ovn
              name: host-config-ovn
            - mountPath: /var/log/ovn
              name: host-log-ovn
            - mountPath: /etc/localtime
              name: localtime
            - mountPath: /var/run/tls
              name: kube-ovn-tls
            - mountPath: /var/log/kube-ovn
              name: kube-ovn-log
      nodeSelector:
        kubernetes.io/os: "linux"
        kube-ovn/role: "master"
      volumes:
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: host-config-ovn
          hostPath:
            path: /etc/origin/ovn
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-log
          hostPath:
            path: /var/log/kube-ovn
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
EOF
kubectl apply -f ovn-ic-controller.yaml
kubectl rollout status deployment/ovn-ic-controller -n kube-system --timeout 60s
fi

echo "-------------------------------"
echo ""

echo "[Step 4/6] Delete pod that not in host network mode"
for ns in $(kubectl get ns --no-headers -o custom-columns=NAME:.metadata.name); do
  for pod in $(kubectl get pod --no-headers -n "$ns" --field-selector spec.restartPolicy=Always -o custom-columns=NAME:.metadata.name,HOST:spec.hostNetwork | awk '{if ($2!="true") print $1}'); do
    kubectl delete pod "$pod" -n "$ns" --ignore-not-found --wait=false
  done
done

kubectl rollout status daemonset/kube-ovn-pinger -n kube-system --timeout 300s
kubectl rollout status deployment/coredns -n kube-system --timeout 600s
echo "-------------------------------"
echo ""

echo "[Step 5/6] Add kubectl plugin PATH"

if ! sh -c "echo \":$PATH:\" | grep -q \":/usr/local/bin:\""; then
  echo "Tips:Please join the /usr/local/bin to your PATH. Temporarily, we do it for this execution."
  export PATH=/usr/local/bin:$PATH
  echo "-------------------------------"
  echo ""
fi

echo "[Step 6/6] Run network diagnose"
kubectl cp kube-system/"$(kubectl -n kube-system get pods -o wide | grep cni | awk '{print $1}' | awk 'NR==1{print}')":/kube-ovn/kubectl-ko /usr/local/bin/kubectl-ko
chmod +x /usr/local/bin/kubectl-ko
# show pod status in kube-system namespace before diagnose
kubectl get pod -n kube-system -o wide
kubectl ko diagnose all

echo "-------------------------------"
echo "
                    ,,,,
                    ,::,
                   ,,::,,,,
            ,,,,,::::::::::::,,,,,
         ,,,::::::::::::::::::::::,,,
       ,,::::::::::::::::::::::::::::,,
     ,,::::::::::::::::::::::::::::::::,,
    ,::::::::::::::::::::::::::::::::::::,
   ,:::::::::::::,,   ,,:::::,,,::::::::::,
 ,,:::::::::::::,       ,::,     ,:::::::::,
 ,:::::::::::::,   :x,  ,::  :,   ,:::::::::,
,:::::::::::::::,  ,,,  ,::, ,,  ,::::::::::,
,:::::::::::::::::,,,,,,:::::,,,,::::::::::::,    ,:,   ,:,            ,xx,                            ,:::::,   ,:,     ,:: :::,    ,x
,::::::::::::::::::::::::::::::::::::::::::::,    :x: ,:xx:        ,   :xx,                          :xxxxxxxxx, :xx,   ,xx:,xxxx,   :x
,::::::::::::::::::::::::::::::::::::::::::::,    :xxxxx:,  ,xx,  :x:  :xxx:x::,  ::xxxx:           :xx:,  ,:xxx  :xx, ,xx: ,xxxxx:, :x
,::::::::::::::::::::::::::::::::::::::::::::,    :xxxxx,   :xx,  :x:  :xxx,,:xx,:xx:,:xx, ,,,,,,,,,xxx,    ,xx:   :xx:xx:  ,xxx,:xx::x
,::::::,,::::::::,,::::::::,,:::::::,,,::::::,    :x:,xxx:  ,xx,  :xx  :xx:  ,xx,xxxxxx:, ,xxxxxxx:,xxx:,  ,xxx,    :xxx:   ,xxx, :xxxx
,::::,    ,::::,   ,:::::,   ,,::::,    ,::::,    :x:  ,:xx,,:xx::xxxx,,xxx::xx: :xx::::x: ,,,,,,   ,xxxxxxxxx,     ,xx:    ,xxx,  :xxx
,::::,    ,::::,    ,::::,    ,::::,    ,::::,    ,:,    ,:,  ,,::,,:,  ,::::,,   ,:::::,            ,,:::::,        ,,      :x:    ,::
,::::,    ,::::,    ,::::,    ,::::,    ,::::,
 ,,,,,    ,::::,    ,::::,    ,::::,    ,:::,             ,,,,,,,,,,,,,
          ,::::,    ,::::,    ,::::,    ,:::,        ,,,:::::::::::::::,
          ,::::,    ,::::,    ,::::,    ,::::,  ,,,,:::::::::,,,,,,,:::,
          ,::::,    ,::::,    ,::::,     ,::::::::::::,,,,,
           ,,,,     ,::::,     ,,,,       ,,,::::,,,,
                    ,::::,
                    ,,::,
"
echo "Thanks for choosing Kube-OVN!
For more advanced features, please read https://kubeovn.github.io/docs/stable/en/
If you have any question, please file an issue https://github.com/kubeovn/kube-ovn/issues/new/choose"
