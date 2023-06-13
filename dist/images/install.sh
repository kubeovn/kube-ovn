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
WITHOUT_KUBE_PROXY=${WITHOUT_KUBE_PROXY:-false}
ENABLE_EXTERNAL_VPC=${ENABLE_EXTERNAL_VPC:-true}
CNI_CONFIG_PRIORITY=${CNI_CONFIG_PRIORITY:-01}
# exchange link names of OVS bridge and the provider nic
# in the default provider-network
# EXCHANGE_LINK_NAME=${EXCHANGE_LINK_NAME:-false}
ENABLE_KEEP_VM_IP=${ENABLE_KEEP_VM_IP:-true}
# exchange link names of OVS bridge and the provider nic
# in the default provider-network
EXCHANGE_LINK_NAME=${EXCHANGE_LINK_NAME:-false}
# The nic to support container network can be a nic name or a group of regex
# separated by comma, if empty will use the nic that the default route use
IFACE=${IFACE:-}
# Specifies the name of the dpdk tunnel iface.
DPDK_TUNNEL_IFACE=${DPDK_TUNNEL_IFACE:-br-phy}
ENABLE_BIND_LOCAL_IP=${ENABLE_BIND_LOCAL_IP:-true}

CNI_CONF_DIR="/etc/cni/net.d"
CNI_BIN_DIR="/opt/cni/bin"

REGISTRY="kubeovn"
VERSION="v1.10.10"
IMAGE_PULL_POLICY="IfNotPresent"
POD_CIDR="10.16.0.0/16"                # Do NOT overlap with NODE/SVC/JOIN CIDR
POD_GATEWAY="10.16.0.1"
SVC_CIDR="10.96.0.0/12"                # Do NOT overlap with NODE/POD/JOIN CIDR
JOIN_CIDR="100.64.0.0/16"              # Do NOT overlap with NODE/POD/SVC CIDR
PINGER_EXTERNAL_ADDRESS="114.114.114.114"  # Pinger check external ip probe
PINGER_EXTERNAL_DOMAIN="alauda.cn"         # Pinger check external domain probe
SVC_YAML_IPFAMILYPOLICY=""
if [ "$IPV6" = "true" ]; then
  POD_CIDR="fd00:10:16::/64"                # Do NOT overlap with NODE/SVC/JOIN CIDR
  POD_GATEWAY="fd00:10:16::1"
  SVC_CIDR="fd00:10:96::/112"               # Do NOT overlap with NODE/POD/JOIN CIDR
  JOIN_CIDR="fd00:100:64::/64"              # Do NOT overlap with NODE/POD/SVC CIDR
  PINGER_EXTERNAL_ADDRESS="2400:3200::1"
  PINGER_EXTERNAL_DOMAIN="google.com"
fi
if [ "$DUAL_STACK" = "true" ]; then
  POD_CIDR="10.16.0.0/16,fd00:10:16::/64"                # Do NOT overlap with NODE/SVC/JOIN CIDR
  POD_GATEWAY="10.16.0.1,fd00:10:16::1"
  SVC_CIDR="10.96.0.0/12,fd00:10:96::/112"               # Do NOT overlap with NODE/POD/JOIN CIDR
  JOIN_CIDR="100.64.0.0/16,fd00:100:64::/64"             # Do NOT overlap with NODE/POD/SVC CIDR
  PINGER_EXTERNAL_ADDRESS="114.114.114.114,2400:3200::1"
  PINGER_EXTERNAL_DOMAIN="google.com"
  SVC_YAML_IPFAMILYPOLICY="ipFamilyPolicy: PreferDualStack"
fi

EXCLUDE_IPS=""                                    # EXCLUDE_IPS for default subnet
LABEL="node-role.kubernetes.io/control-plane"     # The node label to deploy OVN DB
DEPRECATED_LABEL="node-role.kubernetes.io/master" # The node label to deploy OVN DB in earlier versions
NETWORK_TYPE="geneve"                             # geneve or vlan
TUNNEL_TYPE="geneve"                              # geneve, vxlan or stt. ATTENTION: some networkpolicy cannot take effect when using vxlan and stt need custom compile ovs kernel module
POD_NIC_TYPE="veth-pair"                          # veth-pair or internal-port

# VLAN Config only take effect when NETWORK_TYPE is vlan
PROVIDER_NAME="provider"
VLAN_INTERFACE_NAME=""
VLAN_NAME="ovn-vlan"
VLAN_ID="100"

if [ "$ENABLE_VLAN" = "true" ]; then
  NETWORK_TYPE="vlan"
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
        if [[ ! "${DPDK_SUPPORTED_VERSIONS[@]}" = "${DPDK_VERSION}" ]] || [[ -z "${DPDK_VERSION}" ]]; then
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
if [ $count -eq 0 ]; then
  count=$(kubectl get no -l$DEPRECATED_LABEL --no-headers | wc -l)
  node_label="$DEPRECATED_LABEL"
  if [ $count -eq 0 ]; then
    echo "ERROR: No node with label $LABEL or $DEPRECATED_LABEL found"
    exit 1
  fi
fi
kubectl label no -l$node_label kube-ovn/role=master --overwrite

if [ "$DPDK" = "true" -o "$HYBRID_DPDK" = "true" ]; then
  kubectl label no -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel --overwrite
fi

echo "-------------------------------"
echo ""

echo "[Step 2/6] Install OVN components"
addresses=$(kubectl get no -lkube-ovn/role=master --no-headers -o wide | awk '{print $6}' | tr \\n ',')
count=$(kubectl get no -lkube-ovn/role=master --no-headers | wc -l)
echo "Install OVN DB in $addresses"

cat <<EOF > kube-ovn-crd.yaml
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
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                lanIp:
                  type: string
                subnet:
                  type: string
                vpc:
                  type: string
                selector:
                  type: array
                  items:
                    type: string
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
  name: vpcs.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - additionalPrinterColumns:
        - jsonPath: .status.standby
          name: Standby
          type: boolean
        - jsonPath: .status.subnets
          name: Subnets
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
                namespaces:
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
                subnets:
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
        jsonPath: .spec.v4ip
      - name: PV4IP
        type: string
        jsonPath: .spec.parentV4ip
      - name: Mac
        type: string
        jsonPath: .spec.macAddress
      - name: PMac
        type: string
        jsonPath: .spec.ParentMac
      - name: V6IP
        type: string
        jsonPath: .spec.v6ip
      - name: PV6IP
        type: string
        jsonPath: .spec.parentV6ip
      - name: Subnet
        type: string
        jsonPath: .spec.subnet
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
                mac:
                  type: string
                pv4ip:
                  type: string
                pv6ip:
                  type: string
                pmac:
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
                htbqos:
                  type: string
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
                u2oInterconnection:
                  type: boolean
                u2oInterconnectionIP:
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
                      - external
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
  name: htbqoses.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      additionalPrinterColumns:
      - name: PRIORITY
        type: string
        jsonPath: .spec.priority
      schema:
        openAPIV3Schema:
          type: object
          properties:
            spec:
              type: object
              properties:
                priority:
                  type: string					# Value in range 0 to 4,294,967,295.
  scope: Cluster
  names:
    plural: htbqoses
    singular: htbqos
    kind: HtbQos
    shortNames:
      - htbqos
EOF

if $DPDK; then
  cat <<EOF > ovn.yaml
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
  - apiGroups: ['policy']
    resources: ['podsecuritypolicies']
    verbs:     ['use']
    resourceNames:
      - kube-ovn
  - apiGroups:
      - "kubeovn.io"
    resources:
      - vpcs
      - vpcs/status
      - vpc-nat-gateways
      - subnets
      - subnets/status
      - ips
      - vips
      - vips/status
      - vlans
      - vlans/status
      - provider-networks
      - provider-networks/status
      - security-groups
      - security-groups/status
      - htbqoses
      - iptables-eips
      - iptables-fip-rules
      - iptables-dnat-rules
      - iptables-snat-rules
      - iptables-eips/status
      - iptables-fip-rules/status
      - iptables-dnat-rules/status
      - iptables-snat-rules/status
    verbs:
      - "*"
  - apiGroups:
      - ""
    resources:
      - pods
      - pods/exec
      - namespaces
      - nodes
      - configmaps
    verbs:
      - create
      - get
      - list
      - watch
      - patch
      - update
  - apiGroups:
      - "k8s.cni.cncf.io"
    resources:
      - network-attachment-definitions
    verbs:
      - create
      - delete
      - get
      - list
      - update
  - apiGroups:
      - ""
      - networking.k8s.io
      - apps
      - extensions
    resources:
      - networkpolicies
      - services
      - endpoints
      - statefulsets
      - daemonsets
      - deployments
      - deployments/scale
    verbs:
      - create
      - delete
      - update
      - patch
      - get
      - list
      - watch
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
      - "k8s.cni.cncf.io"
    resources:
      - network-attachment-definitions
    verbs:
      - create
      - delete
      - get
      - list
      - update
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
      serviceAccountName: ovn
      hostNetwork: true
      containers:
        - name: ovn-central
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-db.sh"]
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
          resources:
            requests:
              cpu: 300m
              memory: 300Mi
            limits:
              cpu: 3
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
            path: /var/log/openvswitch
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls

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
    type: OnDelete
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
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
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
            path: /var/log/openvswitch
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
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
  cat <<EOF > ovn.yaml
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
  - apiGroups: ['policy']
    resources: ['podsecuritypolicies']
    verbs:     ['use']
    resourceNames:
      - kube-ovn
  - apiGroups:
      - "kubeovn.io"
    resources:
      - vpcs
      - vpcs/status
      - vpc-nat-gateways
      - subnets
      - subnets/status
      - ips
      - vips
      - vips/status
      - vlans
      - vlans/status
      - provider-networks
      - provider-networks/status
      - security-groups
      - security-groups/status
      - htbqoses
      - iptables-eips
      - iptables-fip-rules
      - iptables-dnat-rules
      - iptables-snat-rules
      - iptables-eips/status
      - iptables-fip-rules/status
      - iptables-dnat-rules/status
      - iptables-snat-rules/status
    verbs:
      - "*"
  - apiGroups:
      - ""
    resources:
      - pods
      - pods/exec
      - namespaces
      - nodes
      - configmaps
    verbs:
      - create
      - get
      - list
      - watch
      - patch
      - update
  - apiGroups:
      - ""
      - networking.k8s.io
      - apps
      - extensions
    resources:
      - networkpolicies
      - services
      - endpoints
      - statefulsets
      - daemonsets
      - deployments
      - deployments/scale
    verbs:
      - create
      - delete
      - update
      - patch
      - get
      - list
      - watch
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
      - "k8s.cni.cncf.io"
    resources:
      - network-attachment-definitions
    verbs:
      - create
      - delete
      - get
      - list
      - update
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
      serviceAccountName: ovn
      hostNetwork: true
      containers:
        - name: ovn-central
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-db.sh"]
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
          resources:
            requests:
              cpu: 300m
              memory: 200Mi
            limits:
              cpu: 3
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
            path: /var/log/openvswitch
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
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
    type: OnDelete
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
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
      hostNetwork: true
      hostPID: true
      containers:
        - name: openvswitch
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ovs.sh"]
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
            - name: KUBE_NODE_NAME
              valueFrom:
                fieldRef:
                  fieldPath: spec.nodeName
            - name: OVN_DB_IPS
              value: $addresses
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
            - mountPath: /etc/localtime
              name: localtime
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
              memory: 200Mi
            limits:
              cpu: 1000m
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
            path: /var/log/openvswitch
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
EOF
fi

kubectl apply -f kube-ovn-crd.yaml
kubectl apply -f ovn.yaml

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
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
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
          volumeMounts:
            - mountPath: /opt/ovs-config
              name: host-config-ovs
            - name: shareddir
              mountPath: /var/lib/kubelet/pods
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
            - mountPath: /etc/localtime
              name: localtime
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
            path: /var/lib/kubelet/pods
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
            path: /var/log/openvswitch
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
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
echo "-------------------------------"
echo ""

echo "[Step 3/6] Install Kube-OVN"

cat <<EOF > kube-ovn.yaml
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
          - --pod-nic-type=$POD_NIC_TYPE
          - --enable-lb=$ENABLE_LB
          - --enable-np=$ENABLE_NP
          - --enable-eip-snat=$ENABLE_EIP_SNAT
          - --enable-external-vpc=$ENABLE_EXTERNAL_VPC
          - --logtostderr=false
          - --alsologtostderr=true
          - --log_file=/var/log/kube-ovn/kube-ovn-controller.log
          - --log_file_max_size=0
          - --keep-vm-ip=$ENABLE_KEEP_VM_IP
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
            - mountPath: /var/log/kube-ovn
              name: kube-ovn-log
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
      volumes:
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
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
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
      containers:
      - name: cni-server
        image: "$REGISTRY/kube-ovn:$VERSION"
        imagePullPolicy: $IMAGE_PULL_POLICY
        command:
          - bash
          - /kube-ovn/start-cniserver.sh
        args:
          - --enable-mirror=$ENABLE_MIRROR
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
            mountPath: /var/lib/kubelet/pods
          - mountPath: /etc/openvswitch
            name: systemid
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
            mountPropagation: HostToContainer
          - mountPath: /var/log/kube-ovn
            name: kube-ovn-log
          - mountPath: /var/log/openvswitch
            name: host-log-ovs
          - mountPath: /var/log/ovn
            name: host-log-ovn
          - mountPath: /etc/localtime
            name: localtime
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
          initialDelaySeconds: 30
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
            path: /var/lib/kubelet/pods
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
            path: /var/log/openvswitch
        - name: kube-ovn-log
          hostPath:
            path: /var/log/kube-ovn
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: tmp
          hostPath:
            path: /tmp

---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: kube-ovn-pinger
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      This daemon set launches the openvswitch daemon.
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
      serviceAccountName: ovn
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
            - mountPath: /lib/modules
              name: host-modules
              readOnly: true
            - mountPath: /run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/openvswitch
              name: host-run-ovs
            - mountPath: /var/run/ovn
              name: host-run-ovn
            - mountPath: /sys
              name: host-sys
              readOnly: true
            - mountPath: /etc/openvswitch
              name: host-config-openvswitch
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
            - mountPath: /var/log/ovn
              name: host-log-ovn
            - mountPath: /var/log/kube-ovn
              name: kube-ovn-log
            - mountPath: /etc/localtime
              name: localtime
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
        - name: host-log-ovs
          hostPath:
            path: /var/log/openvswitch
        - name: kube-ovn-log
          hostPath:
            path: /var/log/kube-ovn
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
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
      serviceAccountName: ovn
      hostNetwork: true
      containers:
        - name: kube-ovn-monitor
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ovn-monitor.sh"]
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
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
            - mountPath: /var/log/ovn
              name: host-log-ovn
            - mountPath: /etc/localtime
              name: localtime
            - mountPath: /var/run/tls
              name: kube-ovn-tls
          readinessProbe:
            exec:
              command:
              - cat
              - /var/run/ovn/ovn-controller.pid
            periodSeconds: 10
            timeoutSeconds: 45
          livenessProbe:
            exec:
              command:
              - cat
              - /var/run/ovn/ovn-controller.pid
            initialDelaySeconds: 30
            periodSeconds: 10
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
        - name: host-config-openvswitch
          hostPath:
            path: /etc/origin/openvswitch
        - name: host-config-ovn
          hostPath:
            path: /etc/origin/ovn
        - name: host-log-ovs
          hostPath:
            path: /var/log/openvswitch
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
        - name: localtime
          hostPath:
            path: /etc/localtime
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
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
echo "-------------------------------"
echo ""

echo "[Step 4/6] Delete pod that not in host network mode"
for ns in $(kubectl get ns --no-headers -o custom-columns=NAME:.metadata.name); do
  for pod in $(kubectl get pod --no-headers -n "$ns" --field-selector spec.restartPolicy=Always -o custom-columns=NAME:.metadata.name,HOST:spec.hostNetwork | awk '{if ($2!="true") print $1}'); do
    kubectl delete pod "$pod" -n "$ns" --ignore-not-found
  done
done

sleep 5
kubectl rollout status daemonset/kube-ovn-pinger -n kube-system --timeout 300s
kubectl rollout status deployment/coredns -n kube-system --timeout 300s
echo "-------------------------------"
echo ""

echo "[Step 5/6] Install kubectl plugin"
mkdir -p /usr/local/bin
cat <<\EOF > /usr/local/bin/kubectl-ko
#!/bin/bash
set -euo pipefail

KUBE_OVN_NS=kube-system
EOF
cat <<EOF >> /usr/local/bin/kubectl-ko
WITHOUT_KUBE_PROXY=${WITHOUT_KUBE_PROXY}
EOF
cat <<\EOF >> /usr/local/bin/kubectl-ko
OVN_NB_POD=
OVN_SB_POD=
KUBE_OVN_VERSION=
REGISTRY="kubeovn"

showHelp(){
  echo "kubectl ko {subcommand} [option...]"
  echo "Available Subcommands:"
  echo "  [nb|sb] [status|kick|backup|dbstatus|restore]     ovn-db operations show cluster status, kick stale server, backup database, get db consistency status or restore ovn nb db when met 'inconsistent data' error"
  echo "  nbctl [ovn-nbctl options ...]    invoke ovn-nbctl"
  echo "  sbctl [ovn-sbctl options ...]    invoke ovn-sbctl"
  echo "  vsctl {nodeName} [ovs-vsctl options ...]   invoke ovs-vsctl on the specified node"
  echo "  ofctl {nodeName} [ovs-ofctl options ...]   invoke ovs-ofctl on the specified node"
  echo "  dpctl {nodeName} [ovs-dpctl options ...]   invoke ovs-dpctl on the specified node"
  echo "  appctl {nodeName} [ovs-appctl options ...]   invoke ovs-appctl on the specified node"
  echo "  tcpdump {namespace/podname} [tcpdump options ...]     capture pod traffic"
  echo "  trace {namespace/podname} {target ip address} [target mac address] {icmp|tcp|udp} [target tcp or udp port]    trace ovn microflow of specific packet"
  echo "  diagnose {all|node} [nodename]    diagnose connectivity of all nodes or a specific node"
  echo "  tuning {install-fastpath|local-install-fastpath|remove-fastpath|install-stt|local-install-stt|remove-stt} {centos7|centos8}} [kernel-devel-version]  deploy  kernel optimisation components to the system"
  echo "  reload restart all kube-ovn components"
  echo "  env-check check the environment configuration"
}

# usage: ipv4_to_hex 192.168.0.1
ipv4_to_hex(){
  printf "%02x" ${1//./ }
}

# convert hex to dec (portable version)
hex2dec(){
	for i in $(echo "$@"); do
		printf "%d\n" "$(( 0x$i ))"
	done
}

# https://github.com/chmduquesne/wg-ip
# usage: expand_ipv6 2001::1
expand_ipv6(){
	local ip=$1

	# prepend 0 if we start with :
	echo $ip | grep -qs "^:" && ip="0${ip}"

	# expand ::
	if echo $ip | grep -qs "::"; then
		local colons=$(echo $ip | sed 's/[^:]//g')
		local missing=$(echo ":::::::::" | sed "s/$colons//")
		local expanded=$(echo $missing | sed 's/:/:0/g')
		ip=$(echo $ip | sed "s/::/$expanded/")
	fi

	local blocks=$(echo $ip | grep -o "[0-9a-f]\+")
	set $blocks

	printf "%04x:%04x:%04x:%04x:%04x:%04x:%04x:%04x\n" \
		$(hex2dec $@)
}

# convert an IPv6 address to bytes
ipv6_bytes(){
  for x in $(expand_ipv6 $1 | tr ':' ' '); do
    printf "%d %d " $((0x$x >> 8 & 0xff)) $((0x$x & 0xff))
  done
  echo
}

# usage: ipIsInCidr 192.168.0.1 192.168.0.0/24
# return: 0 for true, 1 for false
ipIsInCidr(){
  local ip=$1
  local cidr=$2

  if [[ $ip =~ .*:.* ]]; then
    # IPv6
    cidr=${cidr#*,}
    local network=${cidr%/*}
    local prefix=${cidr#*/}
    local ip_bytes=($(ipv6_bytes $ip))
    local network_bytes=($(ipv6_bytes $network))
    for ((i=0; i<${#ip_bytes[*]}; i++)); do
      if [ ${ip_bytes[$i]} -eq ${network_bytes[$i]} ]; then
        continue
      fi

      if [ $((($i+1)*8)) -le $prefix ]; then
        return 1
      fi
      if [ $(($i*8)) -ge $prefix ]; then
        return 0
      fi
      if [ $((($i+1)*8)) -le $prefix ]; then
        return 1
      fi

      local bits=$(($prefix-$i*8))
      local mask=$((0xff<<$bits & 0xff))
      # TODO: check whether the IP is network/broadcast address
      if [ $((${ip_bytes[$i]} & $mask)) -ne ${network_bytes[$i]} ]; then
        return 1
      fi
    done

    return 0
  fi

  # IPv4
  cidr=${cidr%,*}
  local network=${cidr%/*}
  local prefix=${cidr#*/}
  local ip_hex=$(ipv4_to_hex $ip)
  local ip_dec=$((0x$ip_hex))
  local network_hex=$(ipv4_to_hex $network)
  local network_dec=$((0x$network_hex))
  local broadcast_dec=$(($network_dec + 2**(32-$prefix) - 1))
  # TODO: check whether the IP is network/broadcast address
  if [ $ip_dec -gt $network_dec -a $ip_dec -lt $broadcast_dec ]; then
    return 0
  fi

  return 1
}

tcpdump(){
  namespacedPod="$1"; shift
  namespace=$(echo "$namespacedPod" | cut -d "/" -f1)
  podName=$(echo "$namespacedPod" | cut -d "/" -f2)
  if [ "$podName" = "$namespacedPod" ]; then
    namespace="default"
  fi

  nodeName=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.nodeName})
  hostNetwork=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.hostNetwork})

  if [ -z "$nodeName" ]; then
    echo "Pod $namespacedPod not exists on any node"
    exit 1
  fi

  ovnCni=$(kubectl get pod -n $KUBE_OVN_NS -l app=kube-ovn-cni -o 'jsonpath={.items[?(@.spec.nodeName=="'$nodeName'")].metadata.name}')
  if [ -z "$ovnCni" ]; then
    echo "kube-ovn-cni not exist on node $nodeName"
    exit 1
  fi

  if [ "$hostNetwork" = "true" ]; then
    set -x
    kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- tcpdump -nn "$@"
  else
    nicName=$(kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- ovs-vsctl --data=bare --no-heading --columns=name find interface external-ids:iface-id="$podName"."$namespace" | tr -d '\r')
    if [ -z "$nicName" ]; then
      echo "nic doesn't exist on node $nodeName"
      exit 1
    fi
    podNicType=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/pod_nic_type})
    podNetNs=$(kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- ovs-vsctl --data=bare --no-heading get interface "$nicName" external-ids:pod_netns | tr -d '\r' | sed -e 's/^"//' -e 's/"$//')
    set -x
    if [ "$podNicType" = "internal-port" ]; then
      kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" tcpdump -nn -i "$nicName" "$@"
    else
      kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" tcpdump -nn -i eth0 "$@"
    fi
  fi
}

trace(){
  namespacedPod="$1"
  namespace=$(echo "$namespacedPod" | cut -d "/" -f1)
  podName=$(echo "$namespacedPod" | cut -d "/" -f2)
  if [ "$podName" = "$namespacedPod" ]; then
    namespace="default"
  fi

  dst="$2"
  if [ -z "$dst" ]; then
    echo "need a target ip address"
    exit 1
  fi

  hostNetwork=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.hostNetwork})
  if [ "$hostNetwork" = "true" ]; then
    echo "Can not trace host network pod"
    exit 1
  fi

  af="4"
  nw="nw"
  proto=""
  if [[ "$dst" =~ .*:.* ]]; then
    af="6"
    nw="ipv6"
    proto="6"
  fi

  podIPs=($(kubectl get pod "$podName" -n "$namespace" -o jsonpath="{.status.podIPs[*].ip}"))
  if [ ${#podIPs[@]} -eq 0 ]; then
    podIPs=($(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/ip_address} | sed 's/,/ /g'))
    if [ ${#podIPs[@]} -eq 0 ]; then
      echo "pod address not ready"
      exit 1
    fi
  fi

  podIP=""
  for ip in ${podIPs[@]}; do
    if [ "$af" = "4" ]; then
      if [[ ! "$ip" =~ .*:.* ]]; then
        podIP=$ip
        break
      fi
    elif [[ "$ip" =~ .*:.* ]]; then
      podIP=$ip
      break
    fi
  done

  if [ -z "$podIP" ]; then
    echo "Pod $namespacedPod has no IPv$af address"
    exit 1
  fi

  nodeName=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.nodeName})
  ovnCni=$(kubectl get pod -n $KUBE_OVN_NS -l app=kube-ovn-cni -o 'jsonpath={.items[?(@.spec.nodeName=="'$nodeName'")].metadata.name}')
  if [ -z "$ovnCni" ]; then
    echo "No kube-ovn-cni Pod running on node $nodeName"
    exit 1
  fi

  ls=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/logical_switch})
  if [ -z "$ls" ]; then
    echo "pod address not ready"
    exit 1
  fi

  local cidr=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/cidr})
  mac=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/mac_address})

  dstMac=""
  if echo "$3" | grep -qE '^([[:xdigit:]]{1,2}:){5}[[:xdigit:]]{1,2}$'; then
    dstMac=$3
    shift
  elif ipIsInCidr $dst $cidr; then
    set +o pipefail
    if [ $af -eq 4 ]; then
      dstMac=$(kubectl exec $OVN_NB_POD -n $KUBE_OVN_NS -c ovn-central -- ovn-nbctl --data=bare --no-heading --columns=addresses list logical_switch_port | grep -w "$(echo $dst | tr . '\.')" | awk '{print $1}')
    else
      dstMac=$(kubectl exec $OVN_NB_POD -n $KUBE_OVN_NS -c ovn-central -- ovn-nbctl --data=bare --no-heading --columns=addresses list logical_switch_port | grep -i " $dst\$" | awk '{print $1}')
    fi
    set -o pipefail
  fi

  if [ -z "$dstMac" ]; then
    vlan=$(kubectl get subnet "$ls" -o jsonpath={.spec.vlan})
    logicalGateway=$(kubectl get subnet "$ls" -o jsonpath={.spec.logicalGateway})
    if [ ! -z "$vlan" -a "$logicalGateway" != "true" ]; then
      gateway=$(kubectl get subnet "$ls" -o jsonpath={.spec.gateway})
      if [[ "$gateway" =~ .*,.* ]]; then
        if [ "$af" = "4" ]; then
          gateway=${gateway%%,*}
        else
          gateway=${gateway##*,}
        fi
      fi

      nicName=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- ovs-vsctl --data=bare --no-heading --columns=name find interface external-ids:iface-id="$podName"."$namespace" | tr -d '\r')
      if [ -z "$nicName" ]; then
        echo "failed to find ovs interface for Pod namespacedPod on node $nodeName"
        exit 1
      fi

      podNicType=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/pod_nic_type})
      podNetNs=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- ovs-vsctl --data=bare --no-heading get interface "$nicName" external-ids:pod_netns | tr -d '\r' | sed -e 's/^"//' -e 's/"$//')
      if [ "$podNicType" != "internal-port" ]; then
        interface=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- ovs-vsctl --format=csv --data=bare --no-heading --columns=name find interface external_id:iface-id="$podName"."$namespace")
        peer=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- ip link show $interface | grep -oE "^[0-9]+:\\s$interface@if[0-9]+" | awk -F @ '{print $2}')
        peerIndex=${peer//if/}
        peer=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" ip link show type veth | grep "^$peerIndex:" | awk -F @ '{print $1}')
        nicName=$(echo $peer | awk '{print $2}')
      fi

      set +o pipefail
      master=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" ip link show $nicName | grep -Eo '\smaster\s\w+\s' | awk '{print $2}')
      set -o pipefail
      if [ ! -z "$master" ]; then
        echo "Error: Pod nic $nicName is a slave of $master, please set the destination mac address."
        exit 1
      fi

      if [[ "$gateway" =~ .*:.* ]]; then
        cmd="ndisc6 -q $gateway $nicName"
        output=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" ndisc6 -q "$gateway" "$nicName")
      else
        cmd="arping -c3 -C1 -i1 -I $nicName $gateway"
        output=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" arping -c3 -C1 -i1 -I "$nicName" "$gateway")
      fi

      if [ $? -ne 0 ]; then
        echo "Error: failed to execute '$cmd' in Pod's netns"
        exit 1
      fi

      dstMac=$(echo "$output" | grep -oE '([[:xdigit:]]{1,2}:){5}[[:xdigit:]]{1,2}')
    fi
  fi

  if [ -z "$dstMac" ]; then
    echo "Using the gateway mac address as destination"
    lr=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/logical_router})
    if [ -z "$lr" ]; then
      lr=$(kubectl get subnet "$ls" -o jsonpath={.spec.vpc})
    fi
    dstMac=$(kubectl exec $OVN_NB_POD -n $KUBE_OVN_NS -c ovn-central -- ovn-nbctl --data=bare --no-heading --columns=mac find logical_router_port name="$lr"-"$ls" | tr -d '\r')
  fi

  if [ -z "$dstMac" ]; then
    echo "failed to get destination mac"
    exit 1
  fi

  lsp="$podName.$namespace"
  lspUUID=$(kubectl exec $OVN_NB_POD -n $KUBE_OVN_NS -c ovn-central -- ovn-nbctl --data=bare --no-heading --columns=_uuid find logical_switch_port name="$lsp")
  if [ -z "$lspUUID" ]; then
    echo "Notice: LSP $lsp does not exist"
  fi
  vmOwner=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath='{.metadata.ownerReferences[?(@.kind=="VirtualMachineInstance")].name}')
  if [ ! -z "$vmOwner" ]; then
    lsp="$vmOwner.$namespace"
  fi

  if [ -z "$lsp" ]; then
    echo "failed to get LSP of Pod $namespace/$podName"
    exit 1
  fi

  type="$3"
  case $type in
    icmp)
      set -x
      kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-trace --ct=new "$ls" "inport == \"$lsp\" && ip.ttl == 64 && icmp && eth.src == $mac && ip$af.src == $podIP && eth.dst == $dstMac && ip$af.dst == $dst"
      ;;
    tcp|udp)
      set -x
      kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-trace --ct=new "$ls" "inport == \"$lsp\" && ip.ttl == 64 && eth.src == $mac && ip$af.src == $podIP && eth.dst == $dstMac && ip$af.dst == $dst && $type.src == 10000 && $type.dst == $4"
      ;;
    *)
      echo "type $type not supported"
      echo "kubectl ko trace {namespace/podname} {target ip address} [target mac address] {icmp|tcp|udp} [target tcp or udp port]"
      exit 1
      ;;
  esac

  set +x
  echo "--------"
  echo "Start OVS Tracing"
  echo ""
  echo ""

  inPort=$(kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- ovs-vsctl --format=csv --data=bare --no-heading --columns=ofport find interface external_id:iface-id="$podName"."$namespace")
  case $type in
    icmp)
      set -x
      kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- ovs-appctl ofproto/trace br-int "in_port=$inPort,icmp$proto,nw_ttl=64,${nw}_src=$podIP,${nw}_dst=$dst,dl_src=$mac,dl_dst=$dstMac"
      ;;
    tcp|udp)
      set -x
      kubectl exec "$ovnCni" -c cni-server -n $KUBE_OVN_NS -- ovs-appctl ofproto/trace br-int "in_port=$inPort,$type$proto,nw_ttl=64,${nw}_src=$podIP,${nw}_dst=$dst,dl_src=$mac,dl_dst=$dstMac,${type}_src=1000,${type}_dst=$4"
      ;;
    *)
      echo "type $type not supported"
      echo "kubectl ko trace {namespace/podname} {target ip address} [target mac address] {icmp|tcp|udp} [target tcp or udp port]"
      exit 1
      ;;
  esac
}

xxctl(){
  subcommand="$1"; shift
  nodeName="$1"; shift
  kubectl get no "$nodeName" > /dev/null
  ovsPod=$(kubectl get pod -n $KUBE_OVN_NS -l app=ovs -o 'jsonpath={.items[?(@.spec.nodeName=="'$nodeName'")].metadata.name}')
  if [ -z "$ovsPod" ]; then
    echo "ovs pod  doesn't exist on node $nodeName"
    exit 1
  fi
  kubectl exec "$ovsPod" -n $KUBE_OVN_NS -- ovs-$subcommand "$@"
}

checkLeader(){
  component="$1"; shift
  set +o pipefail
  count=$(kubectl get ep ovn-$component -n $KUBE_OVN_NS -o yaml | grep ip | wc -l)
  set -o pipefail
  if [ $count -eq 0 ]; then
    echo "no ovn-$component exists !!"
    exit 1
  fi

  if [ $count -gt 1 ]; then
    echo "ovn-$component has more than one leader !!"
    exit 1
  fi

  echo "ovn-$component leader check ok"
}

diagnose(){
  kubectl get crd vpcs.kubeovn.io
  kubectl get crd vpc-nat-gateways.kubeovn.io
  kubectl get crd subnets.kubeovn.io
  kubectl get crd ips.kubeovn.io
  kubectl get crd vlans.kubeovn.io
  kubectl get crd provider-networks.kubeovn.io
  set +eu
  if ! kubectl get svc kube-dns -n kube-system ; then
     echo "Warning: kube-dns doesn't exist, maybe there is coredns service."
  fi
  set -eu
  kubectl get svc kubernetes -n default
  kubectl get sa -n kube-system ovn
  kubectl get clusterrole system:ovn
  kubectl get clusterrolebinding ovn

  kubectl get no -o wide
  kubectl ko nbctl show
  kubectl ko nbctl lr-policy-list ovn-cluster
  kubectl ko nbctl lr-route-list ovn-cluster
  kubectl ko nbctl ls-lb-list ovn-default
  kubectl ko nbctl list address_set
  kubectl ko nbctl list acl
  kubectl ko sbctl show

  if [ "${WITHOUT_KUBE_PROXY}" = "false" ]; then
    checkKubeProxy
  fi

  checkDeployment ovn-central
  checkDeployment kube-ovn-controller
  checkDaemonSet kube-ovn-cni
  checkDaemonSet ovs-ovn
  checkDeployment coredns

  checkLeader nb
  checkLeader sb
  checkLeader northd

  type="$1"
  case $type in
    all)
      echo "### kube-ovn-controller recent log"
      set +e
      kubectl logs -n $KUBE_OVN_NS -l app=kube-ovn-controller --tail=100 | grep E$(date +%m%d)
      set -e
      echo ""
      pingers=$(kubectl -n $KUBE_OVN_NS get po --no-headers -o custom-columns=NAME:.metadata.name -l app=kube-ovn-pinger)
      for pinger in $pingers
      do
        nodeName=$(kubectl get pod "$pinger" -n "$KUBE_OVN_NS" -o jsonpath={.spec.nodeName})
        echo "### start to diagnose node $nodeName"
        echo "#### ovn-controller log:"
        kubectl exec -n $KUBE_OVN_NS "$pinger" -- tail /var/log/ovn/ovn-controller.log
        echo ""
        echo "#### ovs-vswitchd log:"
        kubectl exec -n $KUBE_OVN_NS "$pinger" -- tail /var/log/openvswitch/ovs-vswitchd.log
        echo ""
        echo "#### ovs-vsctl show results:"
        kubectl exec -n $KUBE_OVN_NS "$pinger" -- ovs-vsctl show
        echo ""
        echo "#### pinger diagnose results:"
        kubectl exec -n $KUBE_OVN_NS "$pinger" -- /kube-ovn/kube-ovn-pinger --mode=job
        echo "### finish diagnose node $nodeName"
        echo ""
      done
      ;;
    node)
      nodeName="$2"
      kubectl get no "$nodeName" > /dev/null
      pinger=$(kubectl -n $KUBE_OVN_NS get po -l app=kube-ovn-pinger -o 'jsonpath={.items[?(@.spec.nodeName=="'$nodeName'")].metadata.name}')
      if [ ! -n "$pinger" ]; then
        echo "Error: No kube-ovn-pinger running on node $nodeName"
        exit 1
      fi
      echo "### start to diagnose node $nodeName"
      echo "#### ovn-controller log:"
      kubectl exec -n $KUBE_OVN_NS "$pinger" -- tail /var/log/ovn/ovn-controller.log
      echo ""
      echo "#### ovs-vswitchd log:"
      kubectl exec -n $KUBE_OVN_NS "$pinger" -- tail /var/log/openvswitch/ovs-vswitchd.log
      echo ""
      kubectl exec -n $KUBE_OVN_NS "$pinger" -- /kube-ovn/kube-ovn-pinger --mode=job
      echo "### finish diagnose node $nodeName"
      echo ""
      ;;
    *)
      echo "type $type not supported"
      echo "kubectl ko diagnose {all|node} [nodename]"
      ;;
    esac
}

getOvnCentralPod(){
    NB_POD=$(kubectl get pod -n $KUBE_OVN_NS -l ovn-nb-leader=true | grep ovn-central | head -n 1 | awk '{print $1}')
    if [ -z "$NB_POD" ]; then
      echo "nb leader not exists"
      exit 1
    fi
    OVN_NB_POD=$NB_POD
    SB_POD=$(kubectl get pod -n $KUBE_OVN_NS -l ovn-sb-leader=true | grep ovn-central | head -n 1 | awk '{print $1}')
    if [ -z "$SB_POD" ]; then
      echo "nb leader not exists"
      exit 1
    fi
    OVN_SB_POD=$SB_POD
    VERSION=$(kubectl  -n kube-system get pods -l ovn-sb-leader=true -o yaml | grep  "image: $REGISTRY/kube-ovn:" | head -n 1 | awk -F ':' '{print $3}')
    if [ -z "$VERSION" ]; then
          echo "kubeovn version not exists"
          exit 1
    fi
    KUBE_OVN_VERSION=$VERSION
}

checkDaemonSet(){
  name="$1"
  currentScheduled=$(kubectl get ds -n $KUBE_OVN_NS "$name" -o jsonpath={.status.currentNumberScheduled})
  desiredScheduled=$(kubectl get ds -n $KUBE_OVN_NS "$name" -o jsonpath={.status.desiredNumberScheduled})
  available=$(kubectl get ds -n $KUBE_OVN_NS "$name" -o jsonpath={.status.numberAvailable})
  ready=$(kubectl get ds -n $KUBE_OVN_NS "$name" -o jsonpath={.status.numberReady})
  if [ "$currentScheduled" = "$desiredScheduled" ] && [ "$desiredScheduled" = "$available" ] && [ "$available" = "$ready" ]; then
    echo "ds $name ready"
  else
    echo "Error ds $name not ready"
    exit 1
  fi
}

checkDeployment(){
  name="$1"
  ready=$(kubectl get deployment -n $KUBE_OVN_NS "$name" -o jsonpath={.status.readyReplicas})
  updated=$(kubectl get deployment -n $KUBE_OVN_NS "$name" -o jsonpath={.status.updatedReplicas})
  desire=$(kubectl get deployment -n $KUBE_OVN_NS "$name" -o jsonpath={.status.replicas})
  available=$(kubectl get deployment -n $KUBE_OVN_NS "$name" -o jsonpath={.status.availableReplicas})
  if [ "$ready" = "$updated" ] && [ "$updated" = "$desire" ] && [ "$desire" = "$available" ]; then
    echo "deployment $name ready"
  else
    echo "Error deployment $name not ready"
    exit 1
  fi
}

checkKubeProxy(){
  if kubectl get ds -n kube-system --no-headers -o custom-columns=NAME:.metadata.name | grep '^kube-proxy$' >/dev/null; then
    checkDaemonSet kube-proxy
  else
    for node in $(kubectl get node --no-headers -o custom-columns=NAME:.metadata.name); do
      local pod=$(kubectl get pod -n $KUBE_OVN_NS -l app=kube-ovn-cni -o 'jsonpath={.items[?(@.spec.nodeName=="'$node'")].metadata.name}')
      local ip=$(kubectl get pod -n $KUBE_OVN_NS -l app=kube-ovn-cni -o 'jsonpath={.items[?(@.spec.nodeName=="'$node'")].status.podIP}')
      local arg=""
      if [[ $ip =~ .*:.* ]]; then
        arg="g6"
        ip="[$ip]"
      fi
      healthResult=$(kubectl -n $KUBE_OVN_NS exec $pod -- curl -s${arg} -m 3 -w %{http_code} http://$ip:10256/healthz -o /dev/null | grep -v 200 || true)
      if [ -n "$healthResult" ]; then
        echo "$node kube-proxy's health check failed"
        exit 1
      fi
    done
  fi
  echo "kube-proxy ready"
}

dbtool(){
  suffix=$(date +%m%d%H%M%s)
  component="$1"; shift
  action="$1"; shift
  case $component in
    nb)
      case $action in
        status)
          kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/status OVN_Northbound
          kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status OVN_Northbound
          ;;
        kick)
          kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/kick OVN_Northbound "$1"
          ;;
        backup)
          kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovsdb-tool cluster-to-standalone /etc/ovn/ovnnb_db.$suffix.backup /etc/ovn/ovnnb_db.db
          kubectl cp $KUBE_OVN_NS/$OVN_NB_POD:/etc/ovn/ovnnb_db.$suffix.backup $(pwd)/ovnnb_db.$suffix.backup
          kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- rm -f /etc/ovn/ovnnb_db.$suffix.backup
          echo "backup ovn-$component db to $(pwd)/ovnnb_db.$suffix.backup"
          ;;
        dbstatus)
          kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/get-db-storage-status OVN_Northbound
          ;;
        restore)
          # set ovn-central replicas to 0
          replicas=$(kubectl get deployment -n $KUBE_OVN_NS ovn-central -o jsonpath={.spec.replicas})
          kubectl scale deployment -n $KUBE_OVN_NS ovn-central --replicas=0
          echo "ovn-central original replicas is $replicas"

          # backup ovn-nb db
          declare nodeIpArray
          declare podNameArray
          declare nodeIps

          if [[ $(kubectl get deployment -n kube-system ovn-central -o jsonpath='{.spec.template.spec.containers[0].env[1]}') =~ "NODE_IPS" ]]; then
            nodeIpVals=`kubectl get deployment -n kube-system ovn-central -o jsonpath='{.spec.template.spec.containers[0].env[1].value}'`
            nodeIps=(${nodeIpVals//,/ })
          else
            nodeIps=`kubectl get node -lkube-ovn/role=master -o wide | grep -v "INTERNAL-IP" | awk '{print $6}'`
          fi
          firstIP=${nodeIps[0]}
          podNames=`kubectl get pod -n $KUBE_OVN_NS | grep ovs-ovn | awk '{print $1}'`
          echo "first nodeIP is $firstIP"

          i=0
          for nodeIp in ${nodeIps[@]}
          do
            for pod in $podNames
            do
              hostip=$(kubectl get pod -n $KUBE_OVN_NS $pod -o jsonpath={.status.hostIP})
              if [ $nodeIp = $hostip ]; then
                nodeIpArray[$i]=$nodeIp
                podNameArray[$i]=$pod
                i=`expr $i + 1`
                echo "ovs-ovn pod on node $nodeIp is $pod"
                break
              fi
            done
          done

          echo "backup nb db file"
          kubectl exec -it -n $KUBE_OVN_NS ${podNameArray[0]} -- ovsdb-tool cluster-to-standalone  /etc/ovn/ovnnb_db_standalone.db  /etc/ovn/ovnnb_db.db

          # mv all db files
          for pod in ${podNameArray[@]}
          do
            kubectl exec -it -n $KUBE_OVN_NS $pod -- mv /etc/ovn/ovnnb_db.db /tmp
            kubectl exec -it -n $KUBE_OVN_NS $pod -- mv /etc/ovn/ovnsb_db.db /tmp
          done

          # restore db and replicas
          echo "restore nb db file, operate in pod ${podNameArray[0]}"
          kubectl exec -it -n $KUBE_OVN_NS ${podNameArray[0]} -- mv /etc/ovn/ovnnb_db_standalone.db /etc/ovn/ovnnb_db.db
          kubectl scale deployment -n $KUBE_OVN_NS ovn-central --replicas=$replicas
          echo "finish restore nb db file and ovn-central replicas"

          echo "recreate ovs-ovn pods"
          kubectl delete pod -n $KUBE_OVN_NS -l app=ovs
          ;;
        *)
          echo "unknown action $action"
      esac
      ;;
    sb)
      case $action in
        status)
          kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnsb_db.ctl cluster/status OVN_Southbound
          kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/get-db-storage-status OVN_Southbound
          ;;
        kick)
          kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovs-appctl -t /var/run/ovn/ovnsb_db.ctl cluster/kick OVN_Southbound "$1"
          ;;
        backup)
          kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovsdb-tool cluster-to-standalone /etc/ovn/ovnsb_db.$suffix.backup /etc/ovn/ovnsb_db.db
          kubectl cp $KUBE_OVN_NS/$OVN_SB_POD:/etc/ovn/ovnsb_db.$suffix.backup $(pwd)/ovnsb_db.$suffix.backup
          kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- rm -f /etc/ovn/ovnsb_db.$suffix.backup
          echo "backup ovn-$component db to $(pwd)/ovnsb_db.$suffix.backup"
          ;;
        dbstatus)
          kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/get-db-storage-status OVN_Southbound
          ;;
        restore)
          echo "restore cmd is only used for nb db"
          ;;
        *)
          echo "unknown action $action"
      esac
      ;;
    *)
      echo "unknown subcommand $component"
  esac
}

tuning(){
  action="$1"; shift
  sys="$1"; shift
  case $action in
    install-fastpath)
      case $sys in
        centos7)
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp/:/tmp/ $REGISTRY/centos7-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh  centos install"
          while [ ! -f /tmp/kube_ovn_fastpath.ko ];
          do
            sleep 1
          done
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            kubectl cp /tmp/kube_ovn_fastpath.ko kube-system/"$i":/tmp/
          done
          ;;
        centos8)
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp/:/tmp/ $REGISTRY/centos8-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh  centos install"
          while [ ! -f /tmp/kube_ovn_fastpath.ko ];
          do
            sleep 1
          done
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            kubectl cp /tmp/kube_ovn_fastpath.ko kube-system/"$i":/tmp/
          done
          ;;
        *)
          echo "unknown system $sys"
      esac
      ;;
    local-install-fastpath)
      case $sys in
        centos7)
          # shellcheck disable=SC2145
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp:/tmp $REGISTRY/centos7-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh centos local-install $@"
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            kubectl cp /tmp/kube_ovn_fastpath.ko kube-system/"$i":/tmp/
          done
          ;;
        centos8)
          # shellcheck disable=SC2145
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp:/tmp $REGISTRY/centos8-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh centos local-install $@"
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            kubectl cp /tmp/kube_ovn_fastpath.ko kube-system/"$i":/tmp/
          done
          ;;
        *)
          echo "unknown system $sys"
      esac
      ;;
    remove-fastpath)
      case $sys in
        centos)
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            kubectl -n kube-system exec "$i" -- rm -f /tmp/kube_ovn_fastpath.ko
          done
          ;;
        *)
          echo "unknown system $sys"
      esac
      ;;
    install-stt)
      case $sys in
        centos7)
          # shellcheck disable=SC2145
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp:/tmp $REGISTRY/centos7-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh stt install"
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            for k in /tmp/*.rpm; do
              kubectl cp "$k" kube-system/"$i":/tmp/
            done
          done
          ;;
        centos8)
          # shellcheck disable=SC2145
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp:/tmp $REGISTRY/centos8-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh stt install"
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            for k in /tmp/*.rpm; do
              kubectl cp "$k" kube-system/"$i":/tmp/
            done
          done
          ;;
        *)
          echo "unknown system $sys"
      esac
      ;;
    local-install-stt)
      case $sys in
        centos7)
          # shellcheck disable=SC2145
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp:/tmp $REGISTRY/centos7-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh stt local-install $@"
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            for k in /tmp/*.rpm; do
              kubectl cp "$k" kube-system/"$i":/tmp/
            done
          done
          ;;
        centos8)
          # shellcheck disable=SC2145
          docker run -it --privileged -v /lib/modules:/lib/modules -v /usr/src:/usr/src -v /tmp:/tmp $REGISTRY/centos8-compile:"$KUBE_OVN_VERSION" bash -c "./module.sh stt local-install $@"
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            for k in /tmp/*.rpm; do
              kubectl cp "$k" kube-system/"$i":/tmp/
            done
          done
          ;;
        *)
          echo "unknown system $sys"
      esac
      ;;
    remove-stt)
      case $sys in
        centos)
          for i in $(kubectl -n kube-system get pods | grep ovn-cni | awk '{print $1}');
          do
            kubectl -n kube-system exec "$i" -- rm -f /tmp/openvswitch-kmod*.rpm
          done
          ;;
        *)
          echo "unknown system $sys"
      esac
      ;;
    *)
      echo "unknown action $action"
  esac
}

reload(){
  kubectl delete pod -n kube-system -l app=ovn-central
  kubectl rollout status deployment/ovn-central -n kube-system
  kubectl delete pod -n kube-system -l app=ovs
  kubectl delete pod -n kube-system -l app=kube-ovn-controller
  kubectl rollout status deployment/kube-ovn-controller -n kube-system
  kubectl delete pod -n kube-system -l app=kube-ovn-cni
  kubectl rollout status daemonset/kube-ovn-cni -n kube-system
  kubectl delete pod -n kube-system -l app=kube-ovn-pinger
  kubectl rollout status daemonset/kube-ovn-pinger -n kube-system
  kubectl delete pod -n kube-system -l app=kube-ovn-monitor
  kubectl rollout status deployment/kube-ovn-monitor -n kube-system
}

env-check(){
  set +e

  KUBE_OVN_NS=kube-system
  podNames=`kubectl get pod --no-headers -n $KUBE_OVN_NS | grep kube-ovn-cni | awk '{print $1}'`
  for pod in $podNames
  do
    nodeName=$(kubectl get pod $pod -n $KUBE_OVN_NS -o jsonpath={.spec.nodeName})
    echo "************************************************"
    echo "Start environment check for Node $nodeName"
    echo "************************************************"
    kubectl exec -it -n $KUBE_OVN_NS $pod -c cni-server -- bash /kube-ovn/env-check.sh
  done
}

if [ $# -lt 1 ]; then
  showHelp
  exit 0
else
  subcommand="$1"; shift
fi

getOvnCentralPod

case $subcommand in
  nbctl)
    kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-nbctl "$@"
    ;;
  sbctl)
    kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-sbctl "$@"
    ;;
  vsctl|ofctl|dpctl|appctl)
    xxctl "$subcommand" "$@"
    ;;
  nb|sb)
    dbtool "$subcommand" "$@"
    ;;
  tcpdump)
    tcpdump "$@"
    ;;
  trace)
    trace "$@"
    ;;
  diagnose)
    diagnose "$@"
    ;;
  reload)
    reload
    ;;
  tuning)
    tuning "$@"
    ;;
  env-check)
    env-check
    ;;
  *)
    showHelp
    exit 1
    ;;
esac
EOF

chmod +x /usr/local/bin/kubectl-ko
echo "-------------------------------"
echo ""

if ! sh -c "echo \":$PATH:\" | grep -q \":/usr/local/bin:\""; then
  echo "Tips:Please join the /usr/local/bin to your PATH. Temporarily, we do it for this execution."
  export PATH=/usr/local/bin:$PATH
  echo "-------------------------------"
  echo ""
fi

echo "[Step 6/6] Run network diagnose"
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











