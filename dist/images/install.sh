#!/usr/bin/env bash
set -euo pipefail

REGISTRY="docker.io/kubeovn"
VERSION="v1.15.0"

DEL_NON_HOST_NET_POD=${DEL_NON_HOST_NET_POD:-true}
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
NP_ENFORCEMENT=${NP_ENFORCEMENT:-standard}
ENABLE_EIP_SNAT=${ENABLE_EIP_SNAT:-true}
LS_DNAT_MOD_DL_DST=${LS_DNAT_MOD_DL_DST:-true}
LS_CT_SKIP_DST_LPORT_IPS=${LS_CT_SKIP_DST_LPORT_IPS:-true}
ENABLE_EXTERNAL_VPC=${ENABLE_EXTERNAL_VPC:-false}
CNI_CONFIG_PRIORITY=${CNI_CONFIG_PRIORITY:-01}
ENABLE_LB_SVC=${ENABLE_LB_SVC:-false}
ENABLE_NAT_GW=${ENABLE_NAT_GW:-true}
ENABLE_KEEP_VM_IP=${ENABLE_KEEP_VM_IP:-true}
ENABLE_ARP_DETECT_IP_CONFLICT=${ENABLE_ARP_DETECT_IP_CONFLICT:-true}
ENABLE_METRICS=${ENABLE_METRICS:-true}
# comma-separated string of nodelocal DNS ip addresses
NODE_LOCAL_DNS_IP=${NODE_LOCAL_DNS_IP:-}
# comma-separated list of destination IP CIDRs that should skip conntrack processing
SKIP_CONNTRACK_DST_CIDRS=${SKIP_CONNTRACK_DST_CIDRS:-}
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
SECURE_SERVING=${SECURE_SERVING:-false}
ENABLE_OVN_IPSEC=${ENABLE_OVN_IPSEC:-false}
CERT_MANAGER_IPSEC_CERT=${CERT_MANAGER_IPSEC_CERT:-false}
IPSEC_CERT_DURATION=${IPSEC_CERT_DURATION:-63072000} # 2 years in seconds
CERT_MANAGER_ISSUER_NAME=${CERT_MANAGER_ISSUER_NAME:-kube-ovn}
ENABLE_ANP=${ENABLE_ANP:-false}
ENABLE_DNS_NAME_RESOLVER=${ENABLE_DNS_NAME_RESOLVER:-false}
SET_VXLAN_TX_OFF=${SET_VXLAN_TX_OFF:-false}
OVSDB_CON_TIMEOUT=${OVSDB_CON_TIMEOUT:-3}
OVSDB_INACTIVITY_TIMEOUT=${OVSDB_INACTIVITY_TIMEOUT:-10}
ENABLE_LIVE_MIGRATION_OPTIMIZE=${ENABLE_LIVE_MIGRATION_OPTIMIZE:-true}
ENABLE_OVN_LB_PREFER_LOCAL=${ENABLE_OVN_LB_PREFER_LOCAL:-false}
ENABLE_RECORD_TUNNEL_KEY=${ENABLE_RECORD_TUNNEL_KEY:-false}

PROBE_HTTP_SCHEME="HTTP"
if [ "$SECURE_SERVING" = "true" ]; then
  PROBE_HTTP_SCHEME="HTTPS"
fi

# debug
DEBUG_WRAPPER=${DEBUG_WRAPPER:-}
RUN_AS_USER=65534 # run as nobody
if [ "$ENABLE_OVN_IPSEC" = "true" -o -n "$DEBUG_WRAPPER" ]; then
  RUN_AS_USER=0
fi

KUBELET_DIR=${KUBELET_DIR:-/var/lib/kubelet}
LOG_DIR=${LOG_DIR:-/var/log}

CNI_CONF_DIR="/etc/cni/net.d"
CNI_BIN_DIR="/opt/cni/bin"

VPC_NAT_IMAGE="vpc-nat-gateway"
IMAGE_PULL_POLICY="IfNotPresent"
POD_CIDR="10.16.0.0/16"                     # Do NOT overlap with NODE/SVC/JOIN CIDR
POD_GATEWAY="10.16.0.1"
SVC_CIDR="10.96.0.0/12"                     # Do NOT overlap with NODE/POD/JOIN CIDR
JOIN_CIDR="100.64.0.0/16"                   # Do NOT overlap with NODE/POD/SVC CIDR
PINGER_EXTERNAL_ADDRESS="1.1.1.1"           # Pinger check external ip probe
PINGER_EXTERNAL_DOMAIN="kube-ovn.io."         # Pinger check external domain probe
SVC_YAML_IPFAMILYPOLICY=""
if [ "$IPV6" = "true" ]; then
  POD_CIDR="fd00:10:16::/112"               # Do NOT overlap with NODE/SVC/JOIN CIDR
  POD_GATEWAY="fd00:10:16::1"
  SVC_CIDR="fd00:10:96::/108"               # Do NOT overlap with NODE/POD/JOIN CIDR
  JOIN_CIDR="fd00:100:64::/112"             # Do NOT overlap with NODE/POD/SVC CIDR
  PINGER_EXTERNAL_ADDRESS="2606:4700:4700::1111"
  PINGER_EXTERNAL_DOMAIN="google.com."
fi
if [ "$DUAL_STACK" = "true" ]; then
  POD_CIDR="10.16.0.0/16,fd00:10:16::/112"               # Do NOT overlap with NODE/SVC/JOIN CIDR
  POD_GATEWAY="10.16.0.1,fd00:10:16::1"
  SVC_CIDR="10.96.0.0/12,fd00:10:96::/108"               # Do NOT overlap with NODE/POD/JOIN CIDR
  JOIN_CIDR="100.64.0.0/16,fd00:100:64::/112"            # Do NOT overlap with NODE/POD/SVC CIDR
  PINGER_EXTERNAL_ADDRESS="1.1.1.1,2606:4700:4700::1111"
  PINGER_EXTERNAL_DOMAIN="google.com."
  SVC_YAML_IPFAMILYPOLICY="ipFamilyPolicy: PreferDualStack"
fi

EXCLUDE_IPS=""                                    # EXCLUDE_IPS for default subnet
LABEL="node-role.kubernetes.io/control-plane"     # The node label to deploy OVN DB
DEPRECATED_LABEL="node-role.kubernetes.io/master" # The node label to deploy OVN DB in earlier versions
NETWORK_TYPE="geneve"                             # vlan or (geneve, vxlan or stt)
TUNNEL_TYPE="geneve"                              # (geneve, vxlan or stt). ATTENTION: some networkpolicy cannot take effect when using vxlan and stt need custom compile ovs kernel module
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
DPDK_TAG="${VERSION}-dpdk"
DPDK_CPU="1000m"                        # Default CPU configuration for if --dpdk-cpu flag is not included
DPDK_MEMORY="2Gi"                       # Default Memory configuration for it --dpdk-memory flag is not included

# performance
GC_INTERVAL=360
INSPECT_INTERVAL=20

display_help() {
    echo "Usage: $0 [option...]"
    echo
    echo "  -h, --help               Print Help (this message) and exit"
    echo "  --with-hybrid-dpdk       Install Kube-OVN with nodes which run ovs-dpdk or ovs-kernel"
    echo "  --dpdk-tag=<tag>         Specify the tag of DPDK image, default is $DPDK_TAG"
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
      --dpdk-tag)
        DPDK_TAG="${1#*=}"
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
    if command -v docker &> /dev/null; then
      docker run --rm -v "$PWD":/etc/ovn $REGISTRY/kube-ovn:$VERSION bash generate-ssl.sh
    elif command -v ctr &> /dev/null; then
      ctr image pull $REGISTRY/kube-ovn:$VERSION
      ctr run --rm --mount type=bind,src="$PWD",dst=/etc/ovn,options=rbind:rw $REGISTRY/kube-ovn:$VERSION 0 bash generate-ssl.sh
    else
      echo "ERROR: No docker or ctr found"
      exit 1
    fi
    kubectl create secret generic -n kube-system kube-ovn-tls --from-file=cacert=cacert.pem --from-file=cert=ovn-cert.pem --from-file=key=ovn-privkey.pem
    rm -rf cakey.pem cacert.pem ovn-cert.pem ovn-privkey.pem ovn-req.pem
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
                  description: VPC name for the DNS service. This field is immutable after creation.
                subnet:
                  type: string
                  description: Subnet name for the DNS service. This field is immutable after creation.
                replicas:
                  type: integer
                  description: Number of DNS server replicas (1-3)
                  format: int32
                  minimum: 1
                  maximum: 3
            status:
              type: object
              properties:
                active:
                  type: boolean
                  description: Whether the VPC DNS service is active
                conditions:
                  type: array
                  description: Conditions represent the latest state of the VPC DNS
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of the condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
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
                        description: Port name
                      port:
                        type: integer
                        description: Service port number (1-65535)
                        format: int32
                        minimum: 1
                        maximum: 65535
                      protocol:
                        type: string
                        description: Protocol (TCP or UDP)
                      targetPort:
                        type: integer
                        description: Target port number (1-65535)
                        format: int32
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
                  description: Configured ports
                service:
                  type: string
                  description: Associated service name
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
                  description: External subnets configured for the NAT gateway
                selector:
                  type: array
                  items:
                    type: string
                  description: Pod selector configured for the NAT gateway
                qosPolicy:
                  type: string
                  description: QoS policy applied to the NAT gateway
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
                          - Lt
                          - Gt
                      value:
                        type: string
                      effect:
                        type: string
                        enum:
                          - NoExecute
                          - NoSchedule
                          - PreferNoSchedule
                      tolerationSeconds:
                        format: int64
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
                                type: integer
                                format: int32
                                minimum: 1
                                maximum: 100
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
                                        x-kubernetes-list-type: map
                                        x-kubernetes-list-map-keys:
                                          - key
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
                                type: integer
                                format: int32
                                minimum: 1
                                maximum: 100
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
                                    x-kubernetes-list-type: map
                                    x-kubernetes-list-map-keys:
                                      - key
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
                                        x-kubernetes-list-type: map
                                        x-kubernetes-list-map-keys:
                                          - key
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
                                type: integer
                                format: int32
                                minimum: 1
                                maximum: 100
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
                                    x-kubernetes-list-type: map
                                    x-kubernetes-list-map-keys:
                                      - key
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
                  description: LAN IP address for the NAT gateway. This field is immutable after creation.
                subnet:
                  type: string
                  description: Subnet name for the NAT gateway. This field is immutable after creation.
                externalSubnets:
                  items:
                    type: string
                  type: array
                  description: External subnets accessible through the NAT gateway
                vpc:
                  type: string
                  description: VPC name for the NAT gateway. This field is immutable after creation.
                selector:
                  type: array
                  items:
                    type: string
                  description: Pod selector for the NAT gateway
                qosPolicy:
                  type: string
                  description: QoS policy name to apply to the NAT gateway
                noDefaultEIP:
                  type: boolean
                  description: Disable default EIP assignment
                bgpSpeaker:
                  type: object
                  description: BGP speaker configuration
                  properties:
                    enabled:
                      type: boolean
                      description: Enable BGP speaker
                    asn:
                      type: integer
                      format: uint32
                      description: Local AS number
                    remoteAsn:
                      type: integer
                      format: uint32
                      description: Remote AS number
                    neighbors:
                      type: array
                      items:
                        type: string
                      description: BGP neighbor IP addresses
                    holdTime:
                      type: string
                      description: BGP hold time
                    routerId:
                      type: string
                      description: BGP router ID
                    password:
                      type: string
                      description: BGP authentication password
                    enableGracefulRestart:
                      type: boolean
                      description: Enable BGP graceful restart
                    extraArgs:
                      type: array
                      items:
                        type: string
                      description: Extra BGP arguments
                routes:
                  type: array
                  description: Static routes for the NAT gateway
                  items:
                    type: object
                    properties:
                      cidr:
                        type: string
                        format: cidr
                        description: Destination CIDR for the route
                      nextHopIP:
                        type: string
                        description: Next hop IP address
                annotations:
                  type: object
                  additionalProperties:
                    type: string
                  description: User-defined annotations for the StatefulSet NAT gateway Pod template. Only effective at creation time.
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
                          - Lt
                          - Gt
                      value:
                        type: string
                      effect:
                        type: string
                        enum:
                          - NoExecute
                          - NoSchedule
                          - PreferNoSchedule
                      tolerationSeconds:
                        format: int64
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
                                type: integer
                                format: int32
                                minimum: 1
                                maximum: 100
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
                                        x-kubernetes-list-type: map
                                        x-kubernetes-list-map-keys:
                                          - key
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
                                type: integer
                                format: int32
                                minimum: 1
                                maximum: 100
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
                                    x-kubernetes-list-type: map
                                    x-kubernetes-list-map-keys:
                                      - key
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
                                        x-kubernetes-list-type: map
                                        x-kubernetes-list-map-keys:
                                          - key
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
                                type: integer
                                format: int32
                                minimum: 1
                                maximum: 100
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
                                    x-kubernetes-list-type: map
                                    x-kubernetes-list-map-keys:
                                      - key
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
  name: vpc-egress-gateways.kubeovn.io
spec:
  group: kubeovn.io
  names:
    plural: vpc-egress-gateways
    singular: vpc-egress-gateway
    shortNames:
      - vpc-egress-gw
      - veg
    kind: VpcEgressGateway
    listKind: VpcEgressGatewayList
  scope: Namespaced
  versions:
    - additionalPrinterColumns:
        - jsonPath: .spec.vpc
          name: VPC
          type: string
        - jsonPath: .spec.replicas
          name: REPLICAS
          type: integer
        - jsonPath: .spec.bfd.enabled
          name: BFD ENABLED
          type: boolean
        - jsonPath: .spec.externalSubnet
          name: EXTERNAL SUBNET
          type: string
        - jsonPath: .status.phase
          name: PHASE
          type: string
        - jsonPath: .status.ready
          name: READY
          type: boolean
        - jsonPath: .status.internalIPs
          name: INTERNAL IPS
          priority: 1
          type: string
        - jsonPath: .status.externalIPs
          name: EXTERNAL IPS
          priority: 1
          type: string
        - jsonPath: .status.workload.nodes
          name: WORKING NODES
          priority: 1
          type: string
        - jsonPath: .metadata.creationTimestamp
          name: AGE
          type: date
      name: v1
      served: true
      storage: true
      subresources:
        status: {}
        scale:
          # specReplicasPath defines the JSONPath inside of a custom resource that corresponds to Scale.Spec.Replicas.
          specReplicasPath: .spec.replicas
          # statusReplicasPath defines the JSONPath inside of a custom resource that corresponds to Scale.Status.Replicas.
          statusReplicasPath: .status.replicas
          # labelSelectorPath defines the JSONPath inside of a custom resource that corresponds to Scale.Status.Selector.
          labelSelectorPath: .status.labelSelector
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              properties:
                replicas:
                  type: integer
                  format: int32
                  minimum: 0
                  maximum: 10
                  description: Number of egress gateway replicas
                labelSelector:
                  type: string
                  description: Label selector for the egress gateway
                conditions:
                  items:
                    properties:
                      lastTransitionTime:
                        format: date-time
                        type: string
                      lastUpdateTime:
                        format: date-time
                        type: string
                      message:
                        maxLength: 32768
                        type: string
                      observedGeneration:
                        format: int64
                        minimum: 0
                        type: integer
                      reason:
                        maxLength: 1024
                        minLength: 1
                        pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                        type: string
                      status:
                        enum:
                          - "True"
                          - "False"
                          - Unknown
                        type: string
                      type:
                        maxLength: 316
                        pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
                        type: string
                    required:
                      - lastTransitionTime
                      - lastUpdateTime
                      - observedGeneration
                      - reason
                      - status
                      - type
                    type: object
                  type: array
                  description: Conditions represent the latest available observations of the egress gateway's current state
                  x-kubernetes-list-map-keys:
                    - type
                  x-kubernetes-list-type: map
                internalIPs:
                  items:
                    type: string
                  type: array
                  description: Internal IP addresses assigned to the egress gateway
                externalIPs:
                  items:
                    type: string
                  type: array
                  description: External IP addresses assigned to the egress gateway
                phase:
                  type: string
                  description: Current phase of the egress gateway (Pending, Processing, or Completed)
                  default: Pending
                  enum:
                    - Pending
                    - Processing
                    - Completed
                ready:
                  type: boolean
                  description: Indicates whether the egress gateway is ready
                  default: false
                workload:
                  type: object
                  description: Workload information for the egress gateway
                  properties:
                    apiVersion:
                      type: string
                    kind:
                      type: string
                    name:
                      type: string
                    nodes:
                      type: array
                      items:
                        type: string
              required:
                - conditions
                - phase
              type: object
            spec:
              type: object
              required:
                - externalSubnet
              x-kubernetes-validations:
                - rule: "!has(self.internalIPs) || size(self.internalIPs) == 0 || size(self.internalIPs) >= self.replicas"
                  message: 'Size of Internal IPs MUST be equal to or greater than Replicas'
                  fieldPath: ".internalIPs"
                - rule: "!has(self.externalIPs) || size(self.externalIPs) == 0 || size(self.externalIPs) >= self.replicas"
                  message: 'Size of External IPs MUST be equal to or greater than Replicas'
                  fieldPath: ".externalIPs"
                - rule: "size(self.policies) != 0 || size(self.selectors) != 0"
                  message: 'Each VPC Egress Gateway MUST have at least one policy or selector'
              properties:
                replicas:
                  type: integer
                  format: int32
                  default: 1
                  minimum: 0
                  maximum: 10
                  description: Number of egress gateway replicas
                prefix:
                  type: string
                  description: Name prefix for egress gateway pods. This field is immutable after creation.
                  anyOf:
                    - pattern: ^$
                    - pattern: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*[-\.]?$
                  x-kubernetes-validations:
                    - rule: "self == oldSelf"
                      message: "This field is immutable."
                vpc:
                  type: string
                  description: VPC name for the egress gateway. This field is immutable after creation.
                internalSubnet:
                  type: string
                  description: Internal subnet name for the egress gateway. This field is immutable after creation.
                externalSubnet:
                  type: string
                  description: External subnet name for the egress gateway. This field is immutable after creation and is required.
                internalIPs:
                  items:
                    type: string
                    oneOf:
                      - format: ipv4
                      - format: ipv6
                      - pattern: ^(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5]),((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))$
                      - pattern: ^((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:))),(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])$
                  type: array
                  x-kubernetes-list-type: set
                  description: Internal IP addresses for the egress gateway
                externalIPs:
                  items:
                    type: string
                    oneOf:
                      - format: ipv4
                      - format: ipv6
                      - pattern: ^(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5]),((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))$
                      - pattern: ^((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:))),(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])$
                  type: array
                  x-kubernetes-list-type: set
                  description: External IP addresses for the egress gateway
                image:
                  type: string
                  description: Container image for the egress gateway
                bfd:
                  type: object
                  description: BFD (Bidirectional Forwarding Detection) configuration
                  properties:
                    enabled:
                      type: boolean
                      default: false
                    minRX:
                      type: integer
                      format: int32
                      default: 1000
                      minimum: 1
                      maximum: 3600000
                    minTX:
                      type: integer
                      format: int32
                      default: 1000
                      minimum: 1
                      maximum: 3600000
                    multiplier:
                      type: integer
                      format: int32
                      default: 3
                      minimum: 1
                      maximum: 3600000
                selectors:
                  type: array
                  description: Selectors for pods to use this egress gateway
                  items:
                    type: object
                    properties:
                      namespaceSelector:
                        type: object
                        properties:
                          matchLabels:
                            additionalProperties:
                              type: string
                            type: object
                          matchExpressions:
                            type: array
                            items:
                              type: object
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
                        x-kubernetes-validations:
                          - rule: "size(self.matchLabels) != 0 || size(self.matchExpressions) != 0"
                            message: 'Each namespace selector MUST have at least one matchLabels or matchExpressions'
                      podSelector:
                        type: object
                        properties:
                          matchLabels:
                            additionalProperties:
                              type: string
                            type: object
                          matchExpressions:
                            type: array
                            items:
                              type: object
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
                        x-kubernetes-validations:
                          - rule: "size(self.matchLabels) != 0 || size(self.matchExpressions) != 0"
                            message: 'Each pod selector MUST have at least one matchLabels or matchExpressions'
                policies:
                  type: array
                  description: Egress policies for the gateway
                  items:
                    type: object
                    properties:
                      snat:
                        type: boolean
                        default: false
                      ipBlocks:
                        type: array
                        x-kubernetes-list-type: set
                        items:
                          type: string
                          anyOf:
                            - format: ipv4
                            - format: ipv6
                            - format: cidr
                      subnets:
                        type: array
                        x-kubernetes-list-type: set
                        items:
                          type: string
                          minLength: 1
                    x-kubernetes-validations:
                      - rule: "size(self.ipBlocks) != 0 || size(self.subnets) != 0"
                        message: 'Each policy MUST have at least one ipBlock or subnet'
                trafficPolicy:
                  type: string
                  description: Traffic policy for the egress gateway (Local or Cluster)
                  enum:
                    - Local
                    - Cluster
                  default: Cluster
                nodeSelector:
                  type: array
                  description: Node selector for egress gateway placement
                  items:
                    type: object
                    properties:
                      matchLabels:
                        additionalProperties:
                          type: string
                        type: object
                      matchExpressions:
                        type: array
                        items:
                          type: object
                          properties:
                            key:
                              type: string
                            operator:
                              type: string
                              enum:
                                - In
                                - NotIn
                                - Exists
                                - DoesNotExist
                                - Gt
                                - Lt
                            values:
                              type: array
                              x-kubernetes-list-type: set
                              items:
                                type: string
                          required:
                            - key
                            - operator
                      matchFields:
                        type: array
                        items:
                          type: object
                          properties:
                            key:
                              type: string
                            operator:
                              type: string
                              enum:
                                - In
                                - NotIn
                                - Exists
                                - DoesNotExist
                                - Gt
                                - Lt
                            values:
                              type: array
                              x-kubernetes-list-type: set
                              items:
                                type: string
                          required:
                            - key
                            - operator
                tolerations:
                  description: optional tolerations applied to the workload pods
                  items:
                    description: |-
                      The pod this Toleration is attached to tolerates any taint that matches
                      the triple <key,value,effect> using the matching operator <operator>.
                    properties:
                      effect:
                        description: |-
                          Effect indicates the taint effect to match. Empty means match all taint effects.
                          When specified, allowed values are NoSchedule, PreferNoSchedule and NoExecute.
                        type: string
                        enum:
                          - NoSchedule
                          - PreferNoSchedule
                          - NoExecute
                      key:
                        description: |-
                          Key is the taint key that the toleration applies to. Empty means match all taint keys.
                          If the key is empty, operator must be Exists; this combination means to match all values and all keys.
                        type: string
                      operator:
                        description: |-
                          Operator represents a key's relationship to the value.
                          Valid operators are Exists, Equal, Lt, and Gt. Defaults to Equal.
                          Exists is equivalent to wildcard for value, so that a pod can
                          tolerate all taints of a particular category.
                          Lt and Gt perform numeric comparisons (requires feature gate TaintTolerationComparisonOperators).
                        type: string
                        enum:
                          - Exists
                          - Equal
                          - Lt
                          - Gt
                      tolerationSeconds:
                        description: |-
                          TolerationSeconds represents the period of time the toleration (which must be
                          of effect NoExecute, otherwise this field is ignored) tolerates the taint. By default,
                          it is not set, which means tolerate the taint forever (do not evict). Zero and
                          negative values will be treated as 0 (evict immediately) by the system.
                        format: int64
                        type: integer
                      value:
                        description: |-
                          Value is the taint value the toleration matches to.
                          If the operator is Exists, the value should be empty, otherwise just a regular string.
                        type: string
                    type: object
                  type: array
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
                  description: Indicates whether the EIP is ready
                ip:
                  type: string
                  description: IP address assigned to the EIP
                nat:
                  type: string
                  description: NAT configuration status
                redo:
                  type: string
                  description: Redo operation status
                qosPolicy:
                  type: string
                  description: QoS policy applied to the EIP
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the EIP's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                v4ip:
                  type: string
                  description: IPv4 address for the EIP
                v6ip:
                  type: string
                  description: IPv6 address for the EIP
                macAddress:
                  type: string
                  description: MAC address for the EIP
                natGwDp:
                  type: string
                  description: NAT gateway datapath where the EIP is assigned
                qosPolicy:
                  type: string
                  description: QoS policy name to apply to the EIP
                externalSubnet:
                  type: string
                  description: External subnet name. This field is immutable after creation.
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
                  description: Indicates whether the FIP rule is ready
                v4ip:
                  type: string
                  description: IPv4 address of the EIP
                v6ip:
                  type: string
                  description: IPv6 address of the EIP
                natGwDp:
                  type: string
                  description: NAT gateway datapath where the FIP is configured
                redo:
                  type: string
                  description: Redo operation status
                internalIp:
                  type: string
                  description: Internal IP address mapped to the FIP
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the FIP rule's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                eip:
                  type: string
                  description: EIP name to use for floating IP
                internalIp:
                  type: string
                  description: Internal IP address to map to the floating IP
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
                  description: Indicates whether the DNAT rule is ready
                v4ip:
                  type: string
                  description: IPv4 address of the EIP
                v6ip:
                  type: string
                  description: IPv6 address of the EIP
                natGwDp:
                  type: string
                  description: NAT gateway datapath where the DNAT rule is configured
                redo:
                  type: string
                  description: Redo operation status
                protocol:
                  type: string
                  description: Protocol type of the DNAT rule
                internalIp:
                  type: string
                  description: Internal IP address configured in the DNAT rule
                internalPort:
                  type: string
                  description: Internal port configured in the DNAT rule
                externalPort:
                  type: string
                  description: External port configured in the DNAT rule
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the DNAT rule's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                eip:
                  type: string
                  description: EIP name for DNAT rule
                externalPort:
                  type: string
                  description: External port number
                protocol:
                  type: string
                  description: Protocol type (TCP or UDP)
                internalIp:
                  type: string
                  description: Internal IP address to forward traffic to
                internalPort:
                  type: string
                  description: Internal port number to forward traffic to
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
                  description: Indicates whether the SNAT rule is ready
                v4ip:
                  type: string
                  description: IPv4 address of the EIP
                v6ip:
                  type: string
                  description: IPv6 address of the EIP
                natGwDp:
                  type: string
                  description: NAT gateway datapath where the SNAT rule is configured
                redo:
                  type: string
                  description: Redo operation status
                internalCIDR:
                  type: string
                  description: Internal CIDR configured in the SNAT rule
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the SNAT rule's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                eip:
                  type: string
                  description: EIP name for SNAT rule
                internalCIDR:
                  type: string
                  description: Internal CIDR to be translated via SNAT
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
      - jsonPath: .spec.externalSubnet
        name: ExternalSubnet
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
                  description: Type of the OVN EIP
                nat:
                  type: string
                  description: NAT configuration status
                ready:
                  type: boolean
                  description: Indicates whether the EIP is ready
                v4Ip:
                  type: string
                  description: IPv4 address assigned to the EIP
                v6Ip:
                  type: string
                  description: IPv6 address assigned to the EIP
                macAddress:
                  type: string
                  description: MAC address assigned to the EIP
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the EIP's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                externalSubnet:
                  type: string
                  description: External subnet name. This field is immutable after creation.
                type:
                  type: string
                  description: Type of the OVN EIP (e.g., normal, distributed)
                v4Ip:
                  type: string
                  description: IPv4 address for the EIP
                v6Ip:
                  type: string
                  description: IPv6 address for the EIP
                macAddress:
                  type: string
                  description: MAC address for the EIP
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
      - jsonPath: .spec.type
        name: Type
        type: string
      - jsonPath: .status.v4Eip
        name: V4Eip
        type: string
      - jsonPath: .status.v6Eip
        name: V6Eip
        type: string
      - jsonPath: .status.v4Ip
        name: V4Ip
        type: string
      - jsonPath: .status.v6Ip
        name: V6Ip
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
                  description: Indicates whether the FIP is ready
                v4Eip:
                  type: string
                  description: IPv4 EIP address assigned
                v6Eip:
                  type: string
                  description: IPv6 EIP address assigned
                v4Ip:
                  type: string
                  description: IPv4 address mapped to the FIP
                v6Ip:
                  type: string
                  description: IPv6 address mapped to the FIP
                vpc:
                  type: string
                  description: VPC name where the FIP is configured
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the FIP's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                ovnEip:
                  type: string
                  description: OVN EIP name to use for floating IP
                ipType:
                  type: string
                  description: IP type (e.g., ipv4, ipv6, dual)
                type:
                  type: string
                  description: FIP type
                ipName:
                  type: string
                  description: IP resource name
                vpc:
                  type: string
                  description: VPC name. This field is immutable after creation.
                v4Ip:
                  type: string
                  description: IPv4 address for the floating IP
                v6Ip:
                  type: string
                  description: IPv6 address for the floating IP
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
      - jsonPath: .status.v6Eip
        name: V6Eip
        type: string
      - jsonPath: .status.v4IpCidr
        name: V4IpCidr
        type: string
      - jsonPath: .status.v6IpCidr
        name: V6IpCidr
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
                  description: Indicates whether the SNAT rule is ready
                v4Eip:
                  type: string
                  description: IPv4 EIP address assigned
                v6Eip:
                  type: string
                  description: IPv6 EIP address assigned
                v4IpCidr:
                  type: string
                  description: IPv4 CIDR configured in the SNAT rule
                v6IpCidr:
                  type: string
                  description: IPv6 CIDR configured in the SNAT rule
                vpc:
                  type: string
                  description: VPC name where the SNAT rule is configured
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the SNAT rule's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                ovnEip:
                  type: string
                  description: OVN EIP name for SNAT rule
                vpcSubnet:
                  type: string
                  description: VPC subnet name for SNAT
                ipName:
                  type: string
                  description: IP resource name
                vpc:
                  type: string
                  description: VPC name. This field is immutable after creation.
                v4IpCidr:
                  type: string
                  description: IPv4 CIDR for SNAT
                v6IpCidr:
                  type: string
                  description: IPv6 CIDR for SNAT
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
        - jsonPath: .status.v6Eip
          name: V6Eip
          type: string
        - jsonPath: .status.v4Ip
          name: V4Ip
          type: string
        - jsonPath: .status.v6Ip
          name: V6Ip
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
                  description: Indicates whether the DNAT rule is ready
                v4Eip:
                  type: string
                  description: IPv4 EIP address assigned
                v6Eip:
                  type: string
                  description: IPv6 EIP address assigned
                v4Ip:
                  type: string
                  description: IPv4 address configured in the DNAT rule
                v6Ip:
                  type: string
                  description: IPv6 address configured in the DNAT rule
                vpc:
                  type: string
                  description: VPC name where the DNAT rule is configured
                externalPort:
                  type: string
                  description: External port configured in the DNAT rule
                internalPort:
                  type: string
                  description: Internal port configured in the DNAT rule
                protocol:
                  type: string
                  description: Protocol type configured in the DNAT rule
                ipName:
                  type: string
                  description: IP resource name
                conditions:
                  type: array
                  description: Conditions represent the latest available observations of the DNAT rule's current state
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                ovnEip:
                  type: string
                  description: OVN EIP name for DNAT rule
                ipType:
                  type: string
                  description: IP type (e.g., ipv4, ipv6, dual)
                ipName:
                  type: string
                  description: IP resource name
                externalPort:
                  type: string
                  description: External port number
                internalPort:
                  type: string
                  description: Internal port number to forward traffic to
                protocol:
                  type: string
                  description: Protocol type (TCP or UDP)
                vpc:
                  type: string
                  description: VPC name. This field is immutable after creation.
                v4Ip:
                  type: string
                  description: IPv4 address for DNAT
                v6Ip:
                  type: string
                  description: IPv6 address for DNAT
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
        - jsonPath: .status.defaultLogicalSwitch
          name: DefaultSubnet
          type: string
      name: v1
      schema:
        openAPIV3Schema:
          properties:
            spec:
              properties:
                defaultSubnet:
                  type: string
                  description: The default subnet name for the VPC
                enableExternal:
                  type: boolean
                  description: Enable external network access for the VPC
                enableBfd:
                  type: boolean
                  description: Enable BFD (Bidirectional Forwarding Detection) for the VPC
                namespaces:
                  description: List of namespaces associated with this subnet.
                  items:
                    type: string
                  type: array
                  description: List of namespaces that can use this VPC
                extraExternalSubnets:
                  description: Extra external subnets for provider-network VLAN. Immutable after creation.
                  items:
                    type: string
                  type: array
                staticRoutes:
                  description: Static routes for the VPC.
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
                  description: Policy routes for the VPC.
                  items:
                    properties:
                      priority:
                        type: integer
                        description: Priority of the policy route (0-32767)
                        min: 0
                        max: 32767
                      action:
                        type: string
                      match:
                        type: string
                      nextHopIP:
                        type: string
                    type: object
                  type: array
                vpcPeerings:
                  description: VPC peering configurations.
                  items:
                    properties:
                      remoteVpc:
                        type: string
                      localConnectIP:
                        type: string
                    type: object
                  type: array
                bfdPort:
                  properties:
                    enabled:
                      type: boolean
                      description: Enable BFD port
                      default: false
                    ip:
                      type: string
                      description: IP address for BFD port (IPv4, IPv6, or comma-separated pair)
                      anyOf:
                        - pattern: ^$
                        - pattern: ^(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])$
                        - pattern: ^((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))$
                        - pattern: ^(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5]),((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))$
                        - pattern: ^((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:))),(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])$
                    nodeSelector:
                      properties:
                        matchExpressions:
                          items:
                            properties:
                              key:
                                type: string
                                description: Label key
                              operator:
                                type: string
                                description: Label operator
                                enum:
                                  - In
                                  - NotIn
                                  - Exists
                                  - DoesNotExist
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
                  type: object
                  x-kubernetes-validations:
                    - rule: "self.enabled == false || self.ip != ''"
                      message: 'Port IP must be set when BFD Port is enabled'
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
                  description: Whether this is the default subnet.
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
                  description: Extra external subnets for provider-network VLAN. Immutable after creation.
                  items:
                    type: string
                  type: array
                vpcPeerings:
                  description: VPC peering configurations.
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
                bfdPort:
                  type: object
                  properties:
                    ip:
                      type: string
                      description: BFD port IP address
                    name:
                      type: string
                      description: BFD port name
                    nodes:
                      type: array
                      description: Nodes where BFD port is deployed
                      items:
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
                  description: Pod name that this IP belongs to
                namespace:
                  type: string
                  description: Namespace of the pod
                subnet:
                  type: string
                  description: Primary subnet name for the IP. This field is immutable after creation.
                attachSubnets:
                  type: array
                  description: Additional attached subnets
                  items:
                    type: string
                nodeName:
                  type: string
                  description: Node name where the pod resides
                ipAddress:
                  type: string
                  description: IP address (deprecated, use v4IpAddress or v6IpAddress)
                v4IpAddress:
                  type: string
                  description: IPv4 address
                v6IpAddress:
                  type: string
                  description: IPv6 address
                attachIps:
                  type: array
                  description: Additional IP addresses from attached subnets
                  items:
                    type: string
                macAddress:
                  type: string
                  description: MAC address for the primary IP
                attachMacs:
                  type: array
                  description: MAC addresses for attached IPs
                  items:
                    type: string
                containerID:
                  type: string
                  description: Container ID
                podType:
                  type: string
                  description: Pod type (e.g., pod, vm)
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
      - name: Namespace
        type: string
        jsonPath: .spec.namespace
      - name: V4IP
        type: string
        jsonPath: .status.v4ip
      - name: V6IP
        type: string
        jsonPath: .status.v6ip
      - name: Mac
        type: string
        jsonPath: .status.mac
      - name: Subnet
        type: string
        jsonPath: .spec.subnet
      - name: Type
        type: string
        jsonPath: .status.type
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                type:
                  type: string
                  description: Type of VIP (e.g., Layer2, HealthCheck)
                v4ip:
                  type: string
                  description: Allocated IPv4 address
                v6ip:
                  type: string
                  description: Allocated IPv6 address
                mac:
                  type: string
                  description: MAC address associated with the VIP
                selector:
                  type: array
                  description: Pod names selected by this VIP
                  items:
                    type: string
                conditions:
                  type: array
                  description: Conditions represent the latest state of the VIP
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of the condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                namespace:
                  type: string
                  description: Namespace where the VIP is created. This field is immutable after creation.
                subnet:
                  type: string
                  description: Subnet name for the VIP. This field is immutable after creation.
                type:
                  type: string
                  description: Type of VIP. This field is immutable after creation.
                attachSubnets:
                  type: array
                  description: Additional subnets to attach
                  items:
                    type: string
                v4ip:
                  type: string
                  description: Specific IPv4 address to use (optional, will be allocated if not specified)
                macAddress:
                  type: string
                  description: MAC address for the VIP
                v6ip:
                  type: string
                  description: Specific IPv6 address to use (optional, will be allocated if not specified)
                selector:
                  type: array
                  description: Pod names to be selected by this VIP
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
      - name: Vlan
        type: string
        jsonPath: .spec.vlan
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
            metadata:
              type: object
              properties:
                name:
                  type: string
                  pattern: ^[^0-9]
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
                  description: Underlay to overlay interconnection IP.
                  type: string
                u2oInterconnectionMAC:
                  type: string
                u2oInterconnectionVPC:
                  type: string
                mcastQuerierIP:
                  type: string
                mcastQuerierMAC:
                  type: string
                v4usingIPrange:
                  type: string
                v4availableIPrange:
                  type: string
                v6usingIPrange:
                  type: string
                v6availableIPrange:
                  type: string
                tunnelKey:
                  type: integer
                natOutgoingPolicyRules:
                  description: NAT outgoing policy rules.
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
                  description: VPC name for the subnet. Immutable after creation.
                  type: string
                default:
                  description: Whether this is the default subnet.
                  type: boolean
                protocol:
                  description: Network protocol (IPv4, IPv6, or Dual). Immutable after creation.
                  type: string
                  enum:
                    - IPv4
                    - IPv6
                    - Dual
                cidrBlock:
                  description: CIDR block for the subnet. Immutable after creation.
                  type: string
                namespaces:
                  description: List of namespaces associated with this subnet.
                  type: array
                  items:
                    type: string
                gateway:
                  description: Gateway IP address for the subnet.
                  type: string
                provider:
                  description: Provider network name.
                  type: string
                excludeIps:
                  description: IP addresses to exclude from allocation.
                  type: array
                  items:
                    type: string
                vips:
                  description: Virtual IP addresses for the subnet.
                  type: array
                  items:
                    type: string
                gatewayType:
                  description: Gateway type (distributed or centralized).
                  type: string
                allowSubnets:
                  description: Allowed subnets for east-west traffic.
                  type: array
                  items:
                    type: string
                gatewayNode:
                  description: Gateway node for centralized gateway mode.
                  type: string
                gatewayNodeSelectors:
                  description: Node selectors for gateway placement.
                  type: array
                  items:
                    type: object
                    properties:
                      matchLabels:
                        type: object
                        additionalProperties:
                          type: string
                      matchExpressions:
                        type: array
                        items:
                          type: object
                          properties:
                            key:
                              type: string
                            operator:
                              type: string
                            values:
                              type: array
                              items:
                                type: string
                natOutgoing:
                  description: Enable NAT for outgoing traffic.
                  type: boolean
                externalEgressGateway:
                  description: External egress gateway for the subnet.
                  type: string
                policyRoutingPriority:
                  description: Policy routing priority.
                  type: integer
                  minimum: 1
                  maximum: 32765
                policyRoutingTableID:
                  description: Policy routing table ID.
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
                  description: Maximum transmission unit for the subnet.
                  type: integer
                  minimum: 68
                  maximum: 65535
                private:
                  description: Whether the subnet is private.
                  type: boolean
                vlan:
                  description: VLAN ID for the subnet. Immutable after creation.
                  type: string
                logicalGateway:
                  description: Whether to use logical gateway.
                  type: boolean
                disableGatewayCheck:
                  description: Disable gateway connectivity check.
                  type: boolean
                disableInterConnection:
                  description: Disable subnet interconnection.
                  type: boolean
                enableDHCP:
                  description: Enable DHCP for the subnet.
                  type: boolean
                dhcpV4Options:
                  description: DHCPv4 options for the subnet.
                  type: string
                dhcpV6Options:
                  description: DHCPv6 options for the subnet.
                  type: string
                enableIPv6RA:
                  description: Enable IPv6 router advertisement.
                  type: boolean
                ipv6RAConfigs:
                  description: IPv6 router advertisement configurations.
                  type: string
                allowEWTraffic:
                  description: Allow east-west traffic between pods.
                  type: boolean
                acls:
                  description: Access control lists for the subnet.
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
                  description: NAT outgoing policy rules.
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
                  description: Enable underlay to overlay interconnection.
                  type: boolean
                u2oInterconnectionIP:
                  description: Underlay to overlay interconnection IP.
                  type: string
                enableLb:
                  description: Enable load balancer for the subnet.
                  type: boolean
                enableEcmp:
                  description: Enable ECMP for the subnet.
                  type: boolean
                enableMulticastSnoop:
                  description: Enable multicast snooping.
                  type: boolean
                enableExternalLBAddress:
                  description: Enable external load balancer address.
                  type: boolean
                routeTable:
                  description: Route table for the subnet.
                  type: string
                namespaceSelectors:
                  description: Namespace selectors for subnet association.
                  type: array
                  items:
                    type: object
                    properties:
                      matchLabels:
                        type: object
                        additionalProperties:
                          type: string
                      matchExpressions:
                        type: array
                        items:
                          type: object
                          properties:
                            key:
                              type: string
                            operator:
                              type: string
                            values:
                              type: array
                              items:
                                type: string
                nodeNetwork:
                  description: Node network for the subnet.
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
      - name: enableAddressSet
        type: boolean
        jsonPath: .spec.enableAddressSet
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
                  description: Subnet name for the IP pool. This field is immutable.
                  x-kubernetes-validations:
                    - rule: "self == oldSelf"
                      message: "This field is immutable."
                namespaces:
                  description: List of namespaces associated with this subnet.
                  type: array
                  x-kubernetes-list-type: set
                  description: Namespaces that can use this IP pool
                  items:
                    type: string
                ips:
                  type: array
                  minItems: 1
                  x-kubernetes-list-type: set
                  description: IP addresses or ranges in the pool (IPv4/IPv6 addresses or CIDR ranges)
                  items:
                    type: string
                    anyOf:
                      - format: ipv4
                      - format: ipv6
                      - format: cidr
                      - pattern: ^(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.\.(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])$
                      - pattern: ^((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))\.\.((([0-9A-Fa-f]{1,4}:){7}([0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){6}(:[0-9A-Fa-f]{1,4}|:))|(([0-9A-Fa-f]{1,4}:){5}(((:[0-9A-Fa-f]{1,4}){1,2})|:))|(([0-9A-Fa-f]{1,4}:){4}(((:[0-9A-Fa-f]{1,4}){1,3})|:))|(([0-9A-Fa-f]{1,4}:){3}(((:[0-9A-Fa-f]{1,4}){1,4})|:))|(([0-9A-Fa-f]{1,4}:){2}(((:[0-9A-Fa-f]{1,4}){1,5})|:))|(([0-9A-Fa-f]{1,4}:){1}(((:[0-9A-Fa-f]{1,4}){1,6})|:))|(:(((:[0-9A-Fa-f]{1,4}){1,7})|:)))$
                enableAddressSet:
                  type: boolean
                  default: false
                  description: EnableAddressSet to work with policy-based routing and ACL
              required:
                - subnet
                - ips
            status:
              type: object
              properties:
                v4AvailableIPs:
                  type: number
                  description: Number of available IPv4 addresses
                v4UsingIPs:
                  type: number
                  description: Number of using IPv4 addresses
                v6AvailableIPs:
                  type: number
                  description: Number of available IPv6 addresses
                v6UsingIPs:
                  type: number
                  description: Number of using IPv6 addresses
                v4AvailableIPRange:
                  type: string
                  description: Available IPv4 address range
                v4UsingIPRange:
                  type: string
                  description: IPv4 address range in use
                v6AvailableIPRange:
                  type: string
                  description: Available IPv6 address range
                v6UsingIPRange:
                  type: string
                  description: IPv6 address range in use
                conditions:
                  type: array
                  description: Conditions represent the latest state of the IP pool
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of the condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
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
                  description: VLAN ID (0-4095). This field is immutable after creation.
                  minimum: 0
                  maximum: 4095
                provider:
                  type: string
                  description: Provider network name. This field is immutable after creation.
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
                  description: List of subnet names using this VLAN
                  items:
                    type: string
                conflict:
                  type: boolean
                  description: Whether there is a conflict with this VLAN
      additionalPrinterColumns:
      - name: ID
        type: string
        jsonPath: .spec.id
      - name: Provider
        type: string
        jsonPath: .spec.provider
      - name: conflict
        type: boolean
        jsonPath: .status.conflict
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
                  description: Default interface name for the provider network. This field is immutable after creation.
                  maxLength: 15
                  pattern: '^[^/\s]+$'
                customInterfaces:
                  type: array
                  description: Custom interface configurations for specific nodes
                  items:
                    type: object
                    properties:
                      interface:
                        type: string
                        description: Interface name
                        maxLength: 15
                        pattern: '^[^/\s]+$'
                      nodes:
                        type: array
                        description: Nodes that use this custom interface
                        items:
                          type: string
                exchangeLinkName:
                  type: boolean
                nodeSelector:
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
                      x-kubernetes-list-type: map
                      x-kubernetes-list-map-keys:
                        - key
                    matchLabels:
                      additionalProperties:
                        type: string
                      type: object
                  type: object
                excludeNodes:
                  type: array
                  items:
                    type: string
                autoCreateVlanSubinterfaces:
                  type: boolean
                  description: Automatically create VLAN subinterfaces
                preserveVlanInterfaces:
                  type: boolean
                  description: Enable automatic detection and preservation of VLAN interfaces
                vlanInterfaces:
                  type: array
                  items:
                    type: string
                    pattern: '^[a-zA-Z0-9_-]+\.[0-9]{1,4}$'
                  description: Optional explicit list of VLAN interface names to preserve (e.g., eth0.10, bond0.20)
              required:
                - defaultInterface
            status:
              type: object
              properties:
                ready:
                  type: boolean
                  description: Whether the provider network is ready
                readyNodes:
                  type: array
                  description: Nodes that are ready
                  items:
                    type: string
                notReadyNodes:
                  type: array
                  description: Nodes that are not ready
                  items:
                    type: string
                vlans:
                  type: array
                  description: VLANs in use by this provider network
                  items:
                    type: string
                conditions:
                  type: array
                  description: Conditions of nodes in the provider network
                  items:
                    type: object
                    properties:
                      node:
                        type: string
                        description: Node name
                      type:
                        type: string
                        description: Type of the condition
                      status:
                        type: string
                        description: Status of the condition
                      reason:
                        type: string
                        description: Reason for the condition
                      message:
                        type: string
                        description: Message about the condition
                      lastUpdateTime:
                        type: string
                        description: Last update time
                      lastTransitionTime:
                        type: string
                        description: Last transition time
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
                  description: Ingress traffic rules for the security group
                  items:
                    type: object
                    properties:
                      ipVersion:
                        type: string
                        description: IP version (IPv4 or IPv6)
                      protocol:
                        type: string
                        description: Protocol (tcp, udp, icmp, or all)
                      priority:
                        type: integer
                        description: Rule priority (1-200)
                        min: 1
                        max: 200
                      remoteType:
                        type: string
                        description: Type of remote (address, cidr, or securityGroup)
                      remoteAddress:
                        type: string
                        description: Remote address or CIDR
                      remoteSecurityGroup:
                        type: string
                        description: Remote security group name
                      portRangeMin:
                        type: integer
                        description: Start of port range (1-65535)
                        min: 1
                        max: 65535
                      portRangeMax:
                        type: integer
                        description: End of port range (1-65535)
                        min: 1
                        max: 65535
                      policy:
                        type: string
                        description: Policy action (allow or deny)
                egressRules:
                  type: array
                  description: Egress traffic rules for the security group
                  items:
                    type: object
                    properties:
                      ipVersion:
                        type: string
                        description: IP version (IPv4 or IPv6)
                      protocol:
                        type: string
                        description: Protocol (tcp, udp, icmp, or all)
                      priority:
                        type: integer
                        description: Rule priority (1-200)
                        min: 1
                        max: 200
                      remoteType:
                        type: string
                        description: Type of remote (address, cidr, or securityGroup)
                      remoteAddress:
                        type: string
                        description: Remote address or CIDR
                      remoteSecurityGroup:
                        type: string
                        description: Remote security group name
                      portRangeMin:
                        type: integer
                        description: Start of port range (1-65535)
                        min: 1
                        max: 65535
                      portRangeMax:
                        type: integer
                        description: End of port range (1-65535)
                        min: 1
                        max: 65535
                      policy:
                        type: string
                        description: Policy action (allow or deny)
                allowSameGroupTraffic:
                  type: boolean
                  description: Allow traffic between pods in the same security group
            status:
              type: object
              properties:
                portGroup:
                  type: string
                  description: OVN port group name
                allowSameGroupTraffic:
                  type: boolean
                  description: Current allow same group traffic setting
                ingressMd5:
                  type: string
                  description: MD5 hash of ingress rules
                egressMd5:
                  type: string
                  description: MD5 hash of egress rules
                ingressLastSyncSuccess:
                  type: boolean
                  description: Last ingress sync success status
                egressLastSyncSuccess:
                  type: boolean
                  description: Last egress sync success status
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
                  description: Whether the QoS policy is shared
                bindingType:
                  type: string
                  description: Binding type of the QoS policy
                bandwidthLimitRules:
                  type: array
                  description: Active bandwidth limit rules
                  items:
                    type: object
                    properties:
                      name:
                        type: string
                        description: Rule name
                      interface:
                        type: string
                        description: Interface name
                      rateMax:
                        type: string
                        pattern: '^[0-9]+(\.[0-9]+)?$'
                        description: Maximum rate in Mbps (e.g., 100 or 0.5 for 500Kbps)
                      burstMax:
                        type: string
                        pattern: '^[0-9]+(\.[0-9]+)?$'
                        description: Maximum burst in MB (e.g., 10 or 0.5 for 500KB)
                      priority:
                        type: integer
                        description: Rule priority
                      direction:
                        type: string
                        description: Traffic direction (ingress/egress)
                      matchType:
                        type: string
                        description: Match type
                      matchValue:
                        type: string
                        description: Match value
                conditions:
                  type: array
                  description: Conditions of the QoS policy
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                        description: Type of the condition
                      status:
                        type: string
                        description: Status of the condition (True, False, Unknown)
                      reason:
                        type: string
                        description: Reason for the condition's last transition
                      message:
                        type: string
                        description: Human-readable message indicating details about the condition
                      lastUpdateTime:
                        type: string
                        description: Last time this condition was updated
                      lastTransitionTime:
                        type: string
                        description: Last time the condition transitioned from one status to another
            spec:
              type: object
              properties:
                shared:
                  type: boolean
                  description: Whether the QoS policy is shared across multiple pods
                bindingType:
                  type: string
                  description: Binding type (e.g., pod, namespace)
                bandwidthLimitRules:
                  type: array
                  description: Bandwidth limit rules to apply
                  items:
                    type: object
                    properties:
                      name:
                        type: string
                        description: Rule name
                      interface:
                        type: string
                        description: Network interface to apply the rule
                      rateMax:
                        type: string
                        pattern: '^[0-9]+(\.[0-9]+)?$'
                        description: Maximum rate in Mbps (e.g., 100 or 0.5 for 500Kbps)
                      burstMax:
                        type: string
                        pattern: '^[0-9]+(\.[0-9]+)?$'
                        description: Maximum burst in MB (e.g., 10 or 0.5 for 500KB)
                      priority:
                        type: integer
                        description: Rule priority for ordering
                      direction:
                        type: string
                        description: Traffic direction (ingress or egress)
                      matchType:
                        type: string
                        description: Type of traffic matching (e.g., pod, namespace)
                      matchValue:
                        type: string
                        description: Value to match for the rule
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
automountServiceAccountToken: false
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
    verbs:
      - get
  - apiGroups:
      - discovery.k8s.io
    resources:
      - endpointslices
    verbs:
      - list
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
automountServiceAccountToken: false
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
      - vpc-egress-gateways
      - vpc-egress-gateways/status
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
      - dnsnameresolvers
      - dnsnameresolvers/status
      - qos-policies
      - qos-policies/status
    verbs:
      - create
      - get
      - list
      - update
      - patch
      - watch
      - delete
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
      - list
      - watch
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
      - apps
    resources:
      - deployments
      - deployments/scale
    verbs:
      - get
      - list
      - watch
      - create
      - update
      - delete
  - apiGroups:
      - ""
    resources:
      - services
      - services/status
    verbs:
      - get
      - list
      - update
      - patch
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
      - discovery.k8s.io
    resources:
      - endpointslices
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - apps
    resources:
      - statefulsets
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
      - create
      - update
      - patch
      - get
      - watch
  - apiGroups:
      - "kubevirt.io"
    resources:
      - virtualmachines
      - virtualmachineinstances
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - "policy.networking.k8s.io"
    resources:
      - adminnetworkpolicies
      - baselineadminnetworkpolicies
      - clusternetworkpolicies
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - authentication.k8s.io
    resources:
      - tokenreviews
    verbs:
      - create
  - apiGroups:
      - authorization.k8s.io
    resources:
      - subjectaccessreviews
    verbs:
      - create
  - apiGroups:
      - "certificates.k8s.io"
    resources:
      - "certificatesigningrequests"
    verbs:
      - "get"
      - "list"
      - "watch"
  - apiGroups:
    - certificates.k8s.io
    resources:
    - certificatesigningrequests/status
    - certificatesigningrequests/approval
    verbs:
    - update
  - apiGroups:
    - ""
    resources:
    - secrets
    verbs:
    - get
    - create
  - apiGroups:
    - certificates.k8s.io
    resourceNames:
    - kubeovn.io/signer
    resources:
    - signers
    verbs:
    - approve
    - sign
  - apiGroups:
      - kubevirt.io
    resources:
      - virtualmachineinstancemigrations
    verbs:
      - "list"
      - "watch"
      - "get"
  - apiGroups:
      - apiextensions.k8s.io
    resources:
      - customresourcedefinitions
    verbs:
      - get
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
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ovn
  namespace: kube-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: extension-apiserver-authentication-reader
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
automountServiceAccountToken: false
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
    resources:
      - subnets
      - vlans
      - provider-networks
      - iptables-eips
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
      - nodes/status
      - pods
      - services
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
  - apiGroups:
      - ""
    resources:
      - configmaps
    verbs:
      - get
      - list
      - watch
  - apiGroups:
      - authentication.k8s.io
    resources:
      - tokenreviews
    verbs:
      - create
  - apiGroups:
      - authorization.k8s.io
    resources:
      - subjectaccessreviews
    verbs:
      - create
  - apiGroups:
      - "certificates.k8s.io"
    resources:
      - "certificatesigningrequests"
    verbs:
      - "create"
      - "get"
      - "list"
      - "watch"
      - "delete"
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: secret-reader-ovn-ipsec
  namespace: kube-system
rules:
- apiGroups:
    - ""
  resources:
    - "secrets"
  resourceNames:
    - "ovn-ipsec-ca"
  verbs:
    - "get"
    - "list"
    - "watch"
- apiGroups:
    - "cert-manager.io"
  resources:
    - "certificaterequests"
  verbs:
    - "get"
    - "list"
    - "create"
    - "delete"
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
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-ovn-cni
  namespace: kube-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: extension-apiserver-authentication-reader
subjects:
  - kind: ServiceAccount
    name: kube-ovn-cni
    namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-ovn-cni-secret-reader
  namespace: kube-system
subjects:
- kind: ServiceAccount
  name: kube-ovn-cni
  namespace: kube-system
roleRef:
  kind: Role
  name: secret-reader-ovn-ipsec
  apiGroup: rbac.authorization.k8s.io
EOF

cat <<EOF > kube-ovn-app-sa.yaml
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: kube-ovn-app
  namespace: kube-system
automountServiceAccountToken: false
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
  - apiGroups:
      - authentication.k8s.io
    resources:
      - tokenreviews
    verbs:
      - create
  - apiGroups:
      - authorization.k8s.io
    resources:
      - subjectaccessreviews
    verbs:
      - create
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
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: kube-ovn-app
  namespace: kube-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: extension-apiserver-authentication-reader
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
      automountServiceAccountToken: true
      hostNetwork: true
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: hostpath-init
          image: "$REGISTRY/kube-ovn:$VERSION"
          command:
            - sh
            - -c
            - "chown -R nobody: /var/run/ovn /etc/ovn /var/log/ovn"
          securityContext:
            allowPrivilegeEscalation: true
            capabilities:
              drop:
                - ALL
            privileged: true
            runAsUser: 0
          volumeMounts:
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /etc/ovn
              name: host-config-ovn
            - mountPath: /var/log/ovn
              name: host-log-ovn
      containers:
        - name: ovn-central
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command:
          - bash
          - /kube-ovn/start-db.sh
          securityContext:
            runAsUser: ${RUN_AS_USER}
            privileged: false
            capabilities:
              add:
                - NET_BIND_SERVICE
                - SYS_NICE
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
            - name: OVN_NORTHD_PROBE_INTERVAL
              value: "5000"
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
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /etc/ovn
              name: host-config-ovn
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
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
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
EOF

kubectl apply -f ovn.yaml

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
      automountServiceAccountToken: true
      hostNetwork: true
      hostPID: true
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: hostpath-init
          image: "$REGISTRY/kube-ovn:$VERSION"
          command:
            - sh
            - -xec
            - |
              chmod +t /usr/local/sbin
              chown -R nobody: /var/run/ovn /var/log/ovn /etc/openvswitch /var/run/openvswitch /var/log/openvswitch
              iptables -V
              /usr/share/openvswitch/scripts/ovs-ctl load-kmod
          securityContext:
            allowPrivilegeEscalation: true
            capabilities:
              drop:
                - ALL
            privileged: true
            runAsUser: 0
          volumeMounts:
            - mountPath: /lib/modules
              name: host-modules
              readOnly: true
            - mountPath: /usr/local/sbin
              name: usr-local-sbin
            - mountPath: /var/log/ovn
              name: host-log-ovn
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
      containers:
        - name: openvswitch
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command:
          - /kube-ovn/start-ovs.sh
          securityContext:
            runAsUser: ${RUN_AS_USER}
            privileged: false
            capabilities:
              add:
                - NET_ADMIN
                - NET_BIND_SERVICE
                - NET_RAW
                - SYS_NICE
                - SYS_ADMIN
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
            - name: NODE_NAME
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
            - mountPath: /usr/local/sbin
              name: usr-local-sbin
            - mountPath: /lib/modules
              name: host-modules
              readOnly: true
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
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
                - /kube-ovn/ovs-healthcheck.sh
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
        - name: usr-local-sbin
          emptyDir: {}
        - name: host-modules
          hostPath:
            path: /lib/modules
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

kubectl apply -f ovs-ovn-ds.yaml

if $HYBRID_DPDK; then
cat <<EOF > ovs-ovn-dpdk-ds.yaml
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
      automountServiceAccountToken: true
      hostNetwork: true
      hostPID: true
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: openvswitch
          image: "$REGISTRY/kube-ovn:${DPDK_TAG}"
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
            - name: NODE_NAME
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
                - /kube-ovn/ovs-healthcheck.sh
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
kubectl apply -f ovs-ovn-dpdk-ds.yaml
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
      automountServiceAccountToken: true
      hostNetwork: true
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: hostpath-init
          image: "$REGISTRY/kube-ovn:$VERSION"
          command:
            - sh
            - -c
            - "chown -R nobody: /var/log/kube-ovn"
          securityContext:
            allowPrivilegeEscalation: true
            capabilities:
              drop:
                - ALL
            privileged: true
            runAsUser: 0
          volumeMounts:
            - name: kube-ovn-log
              mountPath: /var/log/kube-ovn
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
          - --np-enforcement=$NP_ENFORCEMENT
          - --enable-eip-snat=$ENABLE_EIP_SNAT
          - --enable-external-vpc=$ENABLE_EXTERNAL_VPC
          - --logtostderr=false
          - --alsologtostderr=true
          - --gc-interval=$GC_INTERVAL
          - --inspect-interval=$INSPECT_INTERVAL
          - --log_file=/var/log/kube-ovn/kube-ovn-controller.log
          - --log_file_max_size=200
          - --enable-lb-svc=$ENABLE_LB_SVC
          - --keep-vm-ip=$ENABLE_KEEP_VM_IP
          - --enable-metrics=$ENABLE_METRICS
          - --node-local-dns-ip=$NODE_LOCAL_DNS_IP
          - --skip-conntrack-dst-cidrs=$SKIP_CONNTRACK_DST_CIDRS
          - --enable-ovn-ipsec=$ENABLE_OVN_IPSEC
          - --cert-manager-ipsec-cert=$CERT_MANAGER_IPSEC_CERT
          - --secure-serving=${SECURE_SERVING}
          - --enable-anp=$ENABLE_ANP
          - --enable-dns-name-resolver=$ENABLE_DNS_NAME_RESOLVER
          - --ovsdb-con-timeout=$OVSDB_CON_TIMEOUT
          - --ovsdb-inactivity-timeout=$OVSDB_INACTIVITY_TIMEOUT
          - --enable-live-migration-optimize=$ENABLE_LIVE_MIGRATION_OPTIMIZE
          - --enable-ovn-lb-prefer-local=$ENABLE_OVN_LB_PREFER_LOCAL
          - --enable-record-tunnel-key=$ENABLE_RECORD_TUNNEL_KEY
          - --image=$REGISTRY/kube-ovn:$VERSION
          securityContext:
            runAsUser: ${RUN_AS_USER}
            privileged: false
            capabilities:
              add:
                - NET_BIND_SERVICE
                - NET_RAW
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: OVN_DB_IPS
              value: $addresses
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
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
            httpGet:
              port: 10660
              path: /readyz
              scheme: ${PROBE_HTTP_SCHEME}
            periodSeconds: 3
            timeoutSeconds: 5
          livenessProbe:
            httpGet:
              port: 10660
              path: /livez
              scheme: ${PROBE_HTTP_SCHEME}
            initialDelaySeconds: 300
            periodSeconds: 7
            failureThreshold: 5
            timeoutSeconds: 5
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
      automountServiceAccountToken: true
      hostNetwork: true
      hostPID: true
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      initContainers:
      - name: hostpath-init
        image: "$REGISTRY/kube-ovn:$VERSION"
        command:
          - sh
          - -xec
          - |
            chmod +t /usr/local/sbin
            iptables -V
        securityContext:
          allowPrivilegeEscalation: true
          capabilities:
            drop:
              - ALL
          privileged: true
          runAsUser: 0
        volumeMounts:
          - name: usr-local-sbin
            mountPath: /usr/local/sbin
          - mountPath: /run/xtables.lock
            name: xtables-lock
            readOnly: false
          - mountPath: /var/run/netns
            name: host-ns
            readOnly: false
          - name: kube-ovn-log
            mountPath: /var/log/kube-ovn
      - name: install-cni
        image: "$REGISTRY/kube-ovn:$VERSION"
        imagePullPolicy: $IMAGE_PULL_POLICY
        command:
          - /kube-ovn/install-cni.sh
          - --cni-conf-name=${CNI_CONFIG_PRIORITY}-kube-ovn.conflist
        env:
          - name: POD_IPS
            valueFrom:
              fieldRef:
                fieldPath: status.podIPs
        securityContext:
          runAsUser: 0
          privileged: true
        volumeMounts:
          - mountPath: /opt/cni/bin
            name: cni-bin
          - mountPath: /etc/cni/net.d
            name: cni-conf
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
          - --logtostderr=false
          - --alsologtostderr=true
          - --log_file=/var/log/kube-ovn/kube-ovn-cni.log
          - --log_file_max_size=200
          - --enable-metrics=$ENABLE_METRICS
          - --kubelet-dir=$KUBELET_DIR
          - --enable-tproxy=$ENABLE_TPROXY
          - --ovs-vsctl-concurrency=$OVS_VSCTL_CONCURRENCY
          - --secure-serving=${SECURE_SERVING}
          - --enable-ovn-ipsec=$ENABLE_OVN_IPSEC
          - --cert-manager-ipsec-cert=$CERT_MANAGER_IPSEC_CERT
          - --ovn-ipsec-cert-duration=$IPSEC_CERT_DURATION
          - --cert-manager-issuer-name=$CERT_MANAGER_ISSUER_NAME
          - --set-vxlan-tx-off=$SET_VXLAN_TX_OFF
        securityContext:
          runAsUser: 0
          privileged: false
          capabilities:
            add:
              - NET_ADMIN
              - NET_BIND_SERVICE
              - NET_RAW
              - SYS_ADMIN
              - SYS_NICE
              - SYS_PTRACE
        env:
          - name: ENABLE_SSL
            value: "$ENABLE_SSL"
          - name: POD_IP
            valueFrom:
              fieldRef:
                fieldPath: status.podIP
          - name: NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
          - name: POD_IPS
            valueFrom:
              fieldRef:
                fieldPath: status.podIPs
          - name: POD_NAME
            valueFrom:
              fieldRef:
                fieldPath: metadata.name
          - name: POD_NAMESPACE
            valueFrom:
              fieldRef:
                fieldPath: metadata.namespace
          - name: ENABLE_BIND_LOCAL_IP
            value: "$ENABLE_BIND_LOCAL_IP"
          - name: DBUS_SYSTEM_BUS_ADDRESS
            value: "unix:path=/host/var/run/dbus/system_bus_socket"
        volumeMounts:
          - name: usr-local-sbin
            mountPath: /usr/local/sbin
          - name: host-modules
            mountPath: /lib/modules
            readOnly: true
          - mountPath: /run/xtables.lock
            name: xtables-lock
            readOnly: false
          - name: shared-dir
            mountPath: $KUBELET_DIR/pods
          - mountPath: /etc/openvswitch
            name: systemid
            readOnly: true
          - mountPath: /etc/ovs_ipsec_keys
            name: ovs-ipsec-keys
          - mountPath: /run/openvswitch
            name: host-run-ovs
            mountPropagation: HostToContainer
          - mountPath: /run/ovn
            name: host-run-ovn
          - mountPath: /host/var/run/dbus
            name: host-dbus
            mountPropagation: HostToContainer
          - mountPath: /var/run/netns
            name: host-ns
            mountPropagation: HostToContainer
          - mountPath: /var/log/kube-ovn
            name: kube-ovn-log
          - mountPath: /var/log/openvswitch
            name: host-log-ovs
          - mountPath: /var/log/ovn
            name: host-log-ovn
          - mountPath: /etc/localtime
            name: localtime
            readOnly: true
        livenessProbe:
          failureThreshold: 3
          initialDelaySeconds: 30
          periodSeconds: 7
          successThreshold: 1
          httpGet:
            port: 10665
            path: /livez
            scheme: ${PROBE_HTTP_SCHEME}
          timeoutSeconds: 5
        readinessProbe:
          failureThreshold: 3
          periodSeconds: 7
          successThreshold: 1
          httpGet:
            port: 10665
            path: /readyz
            scheme: ${PROBE_HTTP_SCHEME}
          timeoutSeconds: 5
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
        - name: usr-local-sbin
          emptyDir: {}
        - name: host-modules
          hostPath:
            path: /lib/modules
        - name: xtables-lock
          hostPath:
            path: /run/xtables.lock
            type: FileOrCreate
        - name: shared-dir
          hostPath:
            path: $KUBELET_DIR/pods
        - name: systemid
          hostPath:
            path: /etc/origin/openvswitch
        - name: ovs-ipsec-keys
          hostPath:
            path: /etc/origin/ovs_ipsec_keys
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
        - name: local-bin
          hostPath:
            path: /usr/local/bin

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
      automountServiceAccountToken: true
      hostNetwork: true
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: hostpath-init
          image: "$REGISTRY/kube-ovn:$VERSION"
          command:
            - sh
            - -c
            - "chown -R nobody: /var/log/kube-ovn"
          securityContext:
            allowPrivilegeEscalation: true
            capabilities:
              drop:
                - ALL
            privileged: true
            runAsUser: 0
          volumeMounts:
            - name: kube-ovn-log
              mountPath: /var/log/kube-ovn
      containers:
        - name: kube-ovn-monitor
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ovn-monitor.sh"]
          args:
          - --secure-serving=${SECURE_SERVING}
          - --log_file=/var/log/kube-ovn/kube-ovn-monitor.log
          - --logtostderr=false
          - --alsologtostderr=true
          - --log_file_max_size=200
          - --enable-metrics=$ENABLE_METRICS
          securityContext:
            runAsUser: ${RUN_AS_USER}
            privileged: false
            capabilities:
              add:
                - NET_BIND_SERVICE
          env:
            - name: ENABLE_SSL
              value: "$ENABLE_SSL"
            - name: NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
            - name: POD_NAME
              valueFrom:
                fieldRef:
                  fieldPath: metadata.name
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
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
            - mountPath: /var/run/ovn
              name: host-run-ovn
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
            httpGet:
              port: 10661
              path: /livez
              scheme: ${PROBE_HTTP_SCHEME}
            timeoutSeconds: 5
          readinessProbe:
            failureThreshold: 3
            initialDelaySeconds: 30
            periodSeconds: 7
            successThreshold: 1
            httpGet:
              port: 10661
              path: /readyz
              scheme: ${PROBE_HTTP_SCHEME}
            timeoutSeconds: 5
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
      automountServiceAccountToken: true
      hostNetwork: true
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: ovn-ic-controller
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ic-controller.sh"]
          args:
          - --log_file=/var/log/kube-ovn/kube-ovn-ic-controller.log
          - --log_file_max_size=200
          - --logtostderr=false
          - --alsologtostderr=true
          securityContext:
            runAsUser: ${RUN_AS_USER}
            privileged: false
            capabilities:
              add:
                - NET_BIND_SERVICE
                - SYS_NICE
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

echo "Check to delete multus pods to reload CNI config"
nad_count=$(
  kubectl get network-attachment-definitions.k8s.cni.cncf.io -A --no-headers 2>/dev/null \
    | wc -l || true
)
nad_count=${nad_count:-0}
if [[ "$nad_count" -gt 0 ]]; then
  echo "Detected $nad_count NetworkAttachmentDefinition(s), restarting Multus pods..."
  kubectl delete pod -n kube-system -l app=multus || true
  kubectl wait --for=condition=Ready pod -n kube-system -l app=multus --timeout=60s
fi

if [ "$DEL_NON_HOST_NET_POD" = "true" ]; then
  echo "[Step 4/6] Delete pod that not in host network mode"
  for ns in $(kubectl get ns --no-headers -o custom-columns=NAME:.metadata.name); do
    for pod in $(kubectl get pod --no-headers -n "$ns" --field-selector spec.restartPolicy=Always -o custom-columns=NAME:.metadata.name,HOST:spec.hostNetwork | awk '{if ($2!="true") print $1}'); do
      kubectl delete pod "$pod" -n "$ns" --ignore-not-found --wait=false
    done
  done
fi

kubectl rollout status deployment/coredns -n kube-system --timeout 300s
while true; do
  pods=(`kubectl get pod -n kube-system -l app=kube-ovn-pinger --template '{{range .items}}{{if .metadata.deletionTimestamp}}{{.metadata.name}}{{"\n"}}{{end}}{{end}}'`)
  if [ ${#pods[@]} -eq 0 ]; then
    break
  fi
  echo "Waiting for ${pods[@]} to be deleted..."
  sleep 1
done

echo "Install Kube-ovn-pinger"
cat <<EOF > kube-ovn-pinger.yaml
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
      automountServiceAccountToken: true
      hostPID: false
      securityContext:
        seccompProfile:
          type: RuntimeDefault
      initContainers:
        - name: hostpath-init
          image: "$REGISTRY/kube-ovn:$VERSION"
          command:
            - sh
            - -c
            - "chown -R nobody: /var/log/kube-ovn"
          securityContext:
            allowPrivilegeEscalation: true
            capabilities:
              drop:
                - ALL
            privileged: true
            runAsUser: 0
          volumeMounts:
            - name: kube-ovn-log
              mountPath: /var/log/kube-ovn
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
          - --log_file_max_size=200
          - --enable-metrics=$ENABLE_METRICS
          imagePullPolicy: $IMAGE_PULL_POLICY
          securityContext:
            runAsUser: ${RUN_AS_USER}
            privileged: false
            capabilities:
              add:
                - NET_BIND_SERVICE
                - NET_RAW
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
            - name: POD_NAMESPACE
              valueFrom:
                fieldRef:
                  fieldPath: metadata.namespace
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
          livenessProbe:
            httpGet:
              path: /metrics
              port: 8080
            initialDelaySeconds: 15
            periodSeconds: 20
          readinessProbe:
            httpGet:
              path: /metrics
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 10
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
EOF

kubectl apply -f kube-ovn-pinger.yaml
kubectl rollout status daemonset/kube-ovn-pinger -n kube-system --timeout 120s
sleep 1
kubectl wait pod --for=condition=Ready -l app=kube-ovn-pinger -n kube-system --timeout 120s
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
