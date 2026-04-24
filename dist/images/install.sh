#!/usr/bin/env bash
set -euo pipefail

REGISTRY="docker.io/kubeovn"
VERSION="v1.17.0"

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
# Note that the dpdk tunnel iface and tunnel ip cidr should be different with Kubernetes api cidr, otherwise the route will be a problem.
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

# BEGIN GENERATED KUBE-OVN CRD BUNDLE
cat <<'EOF' > kube-ovn-crd.yaml
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: bgp-confs.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: BgpConf
    listKind: BgpConfList
    plural: bgp-confs
    singular: bgp-conf
  scope: Cluster
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              connectTime:
                type: string
              ebgpMultiHop:
                type: boolean
              gracefulRestart:
                type: boolean
              holdTime:
                type: string
              keepaliveTime:
                type: string
              localASN:
                format: int32
                type: integer
              neighbours:
                items:
                  type: string
                type: array
              password:
                type: string
              peerASN:
                format: int32
                type: integer
              routerId:
                type: string
            type: object
        type: object
    served: true
    storage: true
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: dnsnameresolvers.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: DNSNameResolver
    listKind: DNSNameResolverList
    plural: dnsnameresolvers
    shortNames:
    - dnr
    singular: dnsnameresolver
  scope: Cluster
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              name:
                description: |-
                  name is the DNS name for which the DNS name resolution information will be stored.
                  For a regular DNS name, only the DNS name resolution information of the regular DNS
                  name will be stored. For a wildcard DNS name, the DNS name resolution information
                  of all the DNS names that match the wildcard DNS name will be stored.
                  For a wildcard DNS name, the '*' will match only one label. Additionally, only a single
                  '*' can be used at the beginning of the wildcard DNS name. For example, '*.example.com.'
                  will match 'sub1.example.com.' but won't match 'sub2.sub1.example.com.'
                maxLength: 254
                pattern: ^(\*\.)?([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.){2,}$
                type: string
                x-kubernetes-validations:
                - message: spec.name is immutable
                  rule: self == oldSelf
            required:
            - name
            type: object
          status:
            description: DNSNameResolverStatus defines the observed status of DNSNameResolver.
            properties:
              resolvedNames:
                description: |-
                  resolvedNames contains a list of matching DNS names and their corresponding IP addresses
                  along with their TTL and last DNS lookup times.
                items:
                  description: DNSNameResolverResolvedName describes the details of
                    a resolved DNS name.
                  properties:
                    conditions:
                      description: |-
                        conditions provide information about the state of the DNS name.
                        Known .status.conditions.type is: "Degraded".
                        "Degraded" is true when the last resolution failed for the DNS name,
                        and false otherwise.
                      items:
                        description: Condition contains details for one aspect of
                          the current state of this API Resource.
                        properties:
                          lastTransitionTime:
                            description: |-
                              lastTransitionTime is the last time the condition transitioned from one status to another.
                              This should be when the underlying condition changed.  If that is not known, then using the time when the API field changed is acceptable.
                            format: date-time
                            type: string
                          message:
                            description: |-
                              message is a human readable message indicating details about the transition.
                              This may be an empty string.
                            maxLength: 32768
                            type: string
                          observedGeneration:
                            description: |-
                              observedGeneration represents the .metadata.generation that the condition was set based upon.
                              For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9, the condition is out of date
                              with respect to the current state of the instance.
                            format: int64
                            minimum: 0
                            type: integer
                          reason:
                            description: |-
                              reason contains a programmatic identifier indicating the reason for the condition's last transition.
                              Producers of specific condition types may define expected values and meanings for this field,
                              and whether the values are considered a guaranteed API.
                              The value should be a CamelCase string.
                              This field may not be empty.
                            maxLength: 1024
                            minLength: 1
                            pattern: ^[A-Za-z]([A-Za-z0-9_,:]*[A-Za-z0-9_])?$
                            type: string
                          status:
                            description: status of the condition, one of True, False,
                              Unknown.
                            enum:
                            - "True"
                            - "False"
                            - Unknown
                            type: string
                          type:
                            description: type of condition in CamelCase or in foo.example.com/CamelCase.
                            maxLength: 316
                            pattern: ^([a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*/)?(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])$
                            type: string
                        required:
                        - lastTransitionTime
                        - message
                        - reason
                        - status
                        - type
                        type: object
                      type: array
                      x-kubernetes-list-map-keys:
                      - type
                      x-kubernetes-list-type: map
                    dnsName:
                      description: |-
                        dnsName is the resolved DNS name matching the name field of DNSNameResolverSpec. This field can
                        store both regular and wildcard DNS names which match the spec.name field. When the spec.name
                        field contains a regular DNS name, this field will store the same regular DNS name after it is
                        successfully resolved. When the spec.name field contains a wildcard DNS name, each resolvedName.dnsName
                        will store the regular DNS names which match the wildcard DNS name and have been successfully resolved.
                        If the wildcard DNS name can also be successfully resolved, then this field will store the wildcard
                        DNS name as well.
                      maxLength: 254
                      pattern: ^(\*\.)?([a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?\.){2,}$
                      type: string
                    resolutionFailures:
                      description: |-
                        resolutionFailures keeps the count of how many consecutive times the DNS resolution failed
                        for the dnsName. If the DNS resolution succeeds then the field will be set to zero. Upon
                        every failure, the value of the field will be incremented by one. The details about the DNS
                        name will be removed, if the value of resolutionFailures reaches 5 and the TTL of all the
                        associated IP addresses have expired.
                      format: int32
                      type: integer
                    resolvedAddresses:
                      description: |-
                        resolvedAddresses gives the list of associated IP addresses and their corresponding TTLs and last
                        lookup times for the dnsName.
                      items:
                        description: DNSNameResolverResolvedAddress describes the
                          details of an IP address for a resolved DNS name.
                        properties:
                          ip:
                            description: |-
                              ip is an IP address associated with the dnsName. The validity of the IP address expires after
                              lastLookupTime + ttlSeconds. To refresh the information, a DNS lookup will be performed upon
                              the expiration of the IP address's validity. If the information is not refreshed then it will
                              be removed with a grace period after the expiration of the IP address's validity.
                            type: string
                          lastLookupTime:
                            description: |-
                              lastLookupTime is the timestamp when the last DNS lookup was completed successfully. The validity of
                              the IP address expires after lastLookupTime + ttlSeconds. The value of this field will be updated to
                              the current time on a successful DNS lookup. If the information is not refreshed then it will be
                              removed with a grace period after the expiration of the IP address's validity.
                            format: date-time
                            type: string
                          ttlSeconds:
                            description: |-
                              ttlSeconds is the time-to-live value of the IP address. The validity of the IP address expires after
                              lastLookupTime + ttlSeconds. On a successful DNS lookup the value of this field will be updated with
                              the current time-to-live value. If the information is not refreshed then it will be removed with a
                              grace period after the expiration of the IP address's validity.
                            format: int32
                            type: integer
                        required:
                        - ip
                        - lastLookupTime
                        - ttlSeconds
                        type: object
                      type: array
                      x-kubernetes-list-map-keys:
                      - ip
                      x-kubernetes-list-type: map
                  required:
                  - dnsName
                  - resolvedAddresses
                  type: object
                type: array
                x-kubernetes-list-map-keys:
                - dnsName
                x-kubernetes-list-type: map
            type: object
        required:
        - spec
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: evpn-confs.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: EvpnConf
    listKind: EvpnConfList
    plural: evpn-confs
    singular: evpn-conf
  scope: Cluster
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              routeTargets:
                items:
                  type: string
                type: array
              vni:
                format: int32
                type: integer
            type: object
        type: object
    served: true
    storage: true
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: ippools.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: IPPool
    listKind: IPPoolList
    plural: ippools
    shortNames:
    - ippool
    singular: ippool
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.subnet
      name: Subnet
      type: string
    - jsonPath: .spec.enableAddressSet
      name: enableAddressSet
      type: boolean
    - jsonPath: .spec.ips
      name: IPs
      type: string
    - jsonPath: .status.v4UsingIPs
      name: V4Used
      type: number
    - jsonPath: .status.v4AvailableIPs
      name: V4Available
      type: number
    - jsonPath: .status.v6UsingIPs
      name: V6Used
      type: number
    - jsonPath: .status.v6AvailableIPs
      name: V6Available
      type: number
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              enableAddressSet:
                default: false
                description: EnableAddressSet to work with policy-based routing and
                  ACL
                type: boolean
              ips:
                description: IP addresses or ranges in the pool (IPv4/IPv6 addresses
                  or CIDR ranges)
                items:
                  type: string
                type: array
              namespaces:
                description: Namespaces that can use this IP pool
                items:
                  type: string
                type: array
              subnet:
                description: Subnet name for the IP pool. This field is immutable.
                type: string
            required:
            - ips
            - subnet
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              v4AvailableIPRange:
                description: Available IPv4 address range
                type: string
              v4AvailableIPs:
                description: Number of available IPv4 addresses
                type: number
              v4UsingIPRange:
                description: IPv4 address range in use
                type: string
              v4UsingIPs:
                description: Number of using IPv4 addresses
                type: number
              v6AvailableIPRange:
                description: Available IPv6 address range
                type: string
              v6AvailableIPs:
                description: Number of available IPv6 addresses
                type: number
              v6UsingIPRange:
                description: IPv6 address range in use
                type: string
              v6UsingIPs:
                description: Number of using IPv6 addresses
                type: number
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: ips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: IP
    listKind: IPList
    plural: ips
    shortNames:
    - ip
    singular: ip
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.v4IpAddress
      name: V4IP
      type: string
    - jsonPath: .spec.v6IpAddress
      name: V6IP
      type: string
    - jsonPath: .spec.macAddress
      name: Mac
      type: string
    - jsonPath: .spec.nodeName
      name: Node
      type: string
    - jsonPath: .spec.subnet
      name: Subnet
      type: string
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              attachIps:
                description: Additional IP addresses from attached subnets
                items:
                  type: string
                type: array
              attachMacs:
                description: MAC addresses for attached IPs
                items:
                  type: string
                type: array
              attachSubnets:
                description: Additional attached subnets
                items:
                  type: string
                type: array
              containerID:
                description: Container ID
                type: string
              ipAddress:
                description: IP address (deprecated, use v4IpAddress or v6IpAddress)
                type: string
              macAddress:
                description: MAC address for the primary IP
                type: string
              namespace:
                description: Namespace of the pod
                type: string
              nodeName:
                description: Node name where the pod resides
                type: string
              podName:
                description: Pod name that this IP belongs to
                type: string
              podType:
                description: Pod type (e.g., pod, vm)
                type: string
              subnet:
                description: Primary subnet name for the IP. This field is immutable
                  after creation.
                type: string
              v4IpAddress:
                description: IPv4 address
                type: string
              v6IpAddress:
                description: IPv6 address
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: iptables-dnat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: IptablesDnatRule
    listKind: IptablesDnatRuleList
    plural: iptables-dnat-rules
    shortNames:
    - dnat
    singular: iptables-dnat-rule
  scope: Cluster
  versions:
  - additionalPrinterColumns:
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              eip:
                description: EIP name for DNAT rule
                type: string
              externalPort:
                description: External port number
                type: string
              internalIp:
                description: Internal IP address to forward traffic to
                type: string
              internalPort:
                description: Internal port number to forward traffic to
                type: string
              protocol:
                description: Protocol type (TCP or UDP)
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              externalPort:
                description: External port configured in the DNAT rule
                type: string
              internalIp:
                description: Internal IP address configured in the DNAT rule
                type: string
              internalPort:
                description: Internal port configured in the DNAT rule
                type: string
              natGwDp:
                description: NatGwDp is the NAT gateway data path
                type: string
              protocol:
                description: Protocol type of the DNAT rule
                type: string
              ready:
                description: Indicates whether the DNAT rule is ready
                type: boolean
              redo:
                description: Redo operation status
                type: string
              v4ip:
                description: V4ip is the IPv4 address of the DNAT rule
                type: string
              v6ip:
                description: V6ip is the IPv6 address of the DNAT rule
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: iptables-eips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: IptablesEIP
    listKind: IptablesEIPList
    plural: iptables-eips
    shortNames:
    - eip
    singular: iptables-eip
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.namespace
      name: Namespace
      type: string
    - jsonPath: .spec.externalSubnet
      name: ExternalSubnet
      type: string
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              externalSubnet:
                description: External subnet name. This field is immutable after creation.
                type: string
              macAddress:
                description: MAC address for the EIP
                type: string
              namespace:
                description: |-
                  Namespace where the NAT gateway StatefulSet/Pod for this EIP resides.
                  If empty, defaults to the kube-ovn controller's own namespace.
                type: string
              natGwDp:
                description: NAT gateway datapath where the EIP is assigned
                type: string
              qosPolicy:
                description: QoS policy name to apply to the EIP
                type: string
              v4ip:
                description: IPv4 address for the EIP
                type: string
              v6ip:
                description: IPv6 address for the EIP
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              ip:
                description: IPv4 address of the EIP
                type: string
              nat:
                description: NAT type (snat or dnat)
                type: string
              qosPolicy:
                description: QoS policy name
                type: string
              ready:
                description: Indicates whether the EIP is ready
                type: boolean
              redo:
                description: Redo operation status
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: iptables-fip-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: IptablesFIPRule
    listKind: IptablesFIPRuleList
    plural: iptables-fip-rules
    shortNames:
    - fip
    singular: iptables-fip-rule
  scope: Cluster
  versions:
  - additionalPrinterColumns:
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              eip:
                description: EIP name to use for floating IP
                type: string
              internalIp:
                description: Internal IP address to map to the floating IP
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              internalIp:
                description: Internal IP address mapped to the FIP
                type: string
              natGwDp:
                description: NAT gateway datapath where the FIP is configured
                type: string
              ready:
                description: Indicates whether the FIP rule is ready
                type: boolean
              redo:
                description: Redo operation status
                type: string
              v4ip:
                description: IPv4 address of the EIP
                type: string
              v6ip:
                description: IPv6 address of the EIP
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: iptables-snat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: IptablesSnatRule
    listKind: IptablesSnatRuleList
    plural: iptables-snat-rules
    shortNames:
    - snat
    singular: iptables-snat-rule
  scope: Cluster
  versions:
  - additionalPrinterColumns:
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              eip:
                description: EIP name for SNAT rule
                type: string
              internalCIDR:
                description: Internal CIDR to be translated via SNAT
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              internalCIDR:
                description: InternalCIDR is the internal CIDR of the SNAT rule
                type: string
              natGwDp:
                description: NatGwDp is the NAT gateway data path
                type: string
              ready:
                description: Indicates whether the SNAT rule is ready
                type: boolean
              redo:
                description: Redo operation status
                type: string
              v4ip:
                description: V4ip is the IPv4 address of the SNAT rule
                type: string
              v6ip:
                description: V6ip is the IPv6 address of the SNAT rule
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: ovn-dnat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: OvnDnatRule
    listKind: OvnDnatRuleList
    plural: ovn-dnat-rules
    shortNames:
    - odnat
    singular: ovn-dnat-rule
  scope: Cluster
  versions:
  - additionalPrinterColumns:
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              externalPort:
                description: External port number
                type: string
              internalPort:
                description: Internal port number to forward traffic to
                type: string
              ipName:
                description: IP resource name
                type: string
              ipType:
                description: IP type (e.g., ipv4, ipv6, dual)
                type: string
              ovnEip:
                description: OVN EIP name for DNAT rule
                type: string
              protocol:
                description: Protocol type (TCP or UDP)
                type: string
              v4Ip:
                description: IPv4 address for DNAT
                type: string
              v6Ip:
                description: IPv6 address for DNAT
                type: string
              vpc:
                description: VPC name. This field is immutable after creation.
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              externalPort:
                description: ExternalPort is the external port of the DNAT rule
                type: string
              internalPort:
                description: InternalPort is the internal port of the DNAT rule
                type: string
              ipName:
                description: IP resource name
                type: string
              protocol:
                description: Protocol of the DNAT rule
                type: string
              ready:
                description: Indicates whether the DNAT rule is ready
                type: boolean
              v4Eip:
                description: V4Eip is the IPv4 EIP address
                type: string
              v4Ip:
                description: V4Ip is the IPv4 address of the DNAT rule
                type: string
              v6Eip:
                description: V6Eip is the IPv6 EIP address
                type: string
              v6Ip:
                description: V6Ip is the IPv6 address of the DNAT rule
                type: string
              vpc:
                description: VPC name where the DNAT rule is configured
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: ovn-eips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: OvnEip
    listKind: OvnEipList
    plural: ovn-eips
    shortNames:
    - oeip
    singular: ovn-eip
  scope: Cluster
  versions:
  - additionalPrinterColumns:
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              externalSubnet:
                description: External subnet name. This field is immutable after creation.
                type: string
              macAddress:
                description: MAC address for the EIP
                type: string
              type:
                description: Type of the OVN EIP (e.g., normal, distributed)
                type: string
              v4Ip:
                description: IPv4 address for the EIP
                type: string
              v6Ip:
                description: IPv6 address for the EIP
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              macAddress:
                description: MAC address assigned to the EIP
                type: string
              nat:
                description: NAT configuration status
                type: string
              ready:
                description: Indicates whether the EIP is ready
                type: boolean
              type:
                description: Type of the OVN EIP
                type: string
              v4Ip:
                description: IPv4 address assigned to the EIP
                type: string
              v6Ip:
                description: IPv6 address assigned to the EIP
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: ovn-fips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: OvnFip
    listKind: OvnFipList
    plural: ovn-fips
    shortNames:
    - ofip
    singular: ovn-fip
  scope: Cluster
  versions:
  - additionalPrinterColumns:
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              ipName:
                description: IP resource name
                type: string
              ipType:
                description: IP type (e.g., ipv4, ipv6, dual)
                type: string
              ovnEip:
                description: OVN EIP name to use for floating IP
                type: string
              type:
                description: FIP type
                type: string
              v4Ip:
                description: IPv4 address for the floating IP
                type: string
              v6Ip:
                description: IPv6 address for the floating IP
                type: string
              vpc:
                description: VPC name. This field is immutable after creation.
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              macAddress:
                description: MacAddress of the FIP
                type: string
              ready:
                description: Indicates whether the FIP rule is ready
                type: boolean
              v4Eip:
                description: V4Eip is the IPv4 EIP address
                type: string
              v4Ip:
                description: V4Ip is the IPv4 address of the FIP
                type: string
              v6Eip:
                description: V6Eip is the IPv6 EIP address
                type: string
              v6Ip:
                description: V6Ip is the IPv6 address of the FIP
                type: string
              vpc:
                description: VPC name where the FIP is configured
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: ovn-snat-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: OvnSnatRule
    listKind: OvnSnatRuleList
    plural: ovn-snat-rules
    shortNames:
    - osnat
    singular: ovn-snat-rule
  scope: Cluster
  versions:
  - additionalPrinterColumns:
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
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              ipName:
                description: IP resource name
                type: string
              ovnEip:
                description: OVN EIP name for SNAT rule
                type: string
              v4IpCidr:
                description: IPv4 CIDR for SNAT
                type: string
              v6IpCidr:
                description: IPv6 CIDR for SNAT
                type: string
              vpc:
                description: VPC name. This field is immutable after creation.
                type: string
              vpcSubnet:
                description: VPC subnet name for SNAT
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              ready:
                description: Indicates whether the SNAT rule is ready
                type: boolean
              v4Eip:
                description: V4Eip is the IPv4 EIP address
                type: string
              v4IpCidr:
                description: V4IpCidr is the IPv4 CIDR of the SNAT rule
                type: string
              v6Eip:
                description: V6Eip is the IPv6 EIP address
                type: string
              v6IpCidr:
                description: V6IpCidr is the IPv6 CIDR of the SNAT rule
                type: string
              vpc:
                description: VPC name where the SNAT rule is configured
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: provider-networks.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: ProviderNetwork
    listKind: ProviderNetworkList
    plural: provider-networks
    shortNames:
    - pn
    singular: provider-network
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.defaultInterface
      name: DefaultInterface
      type: string
    - jsonPath: .status.ready
      name: Ready
      type: boolean
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              autoCreateVlanSubinterfaces:
                description: Automatically create VLAN subinterfaces
                type: boolean
              customInterfaces:
                description: Custom interface configurations for specific nodes
                items:
                  properties:
                    interface:
                      description: Interface name
                      maxLength: 15
                      pattern: ^[^/\s]+$
                      type: string
                    nodes:
                      description: Nodes that use this custom interface
                      items:
                        type: string
                      type: array
                  type: object
                type: array
              defaultInterface:
                description: Default interface name for the provider network. This
                  field is immutable after creation.
                maxLength: 15
                pattern: ^[^/\s]+$
                type: string
              exchangeLinkName:
                description: Exchange link name between host and container
                type: boolean
              excludeNodes:
                description: Nodes to exclude from this provider network
                items:
                  type: string
                type: array
              nodeSelector:
                description: Node selector for targeting specific nodes
                properties:
                  matchExpressions:
                    description: matchExpressions is a list of label selector requirements.
                      The requirements are ANDed.
                    items:
                      description: |-
                        A label selector requirement is a selector that contains values, a key, and an operator that
                        relates the key and values.
                      properties:
                        key:
                          description: key is the label key that the selector applies
                            to.
                          type: string
                        operator:
                          description: |-
                            operator represents a key's relationship to a set of values.
                            Valid operators are In, NotIn, Exists and DoesNotExist.
                          type: string
                        values:
                          description: |-
                            values is an array of string values. If the operator is In or NotIn,
                            the values array must be non-empty. If the operator is Exists or DoesNotExist,
                            the values array must be empty. This array is replaced during a strategic
                            merge patch.
                          items:
                            type: string
                          type: array
                          x-kubernetes-list-type: atomic
                      required:
                      - key
                      - operator
                      type: object
                    type: array
                    x-kubernetes-list-type: atomic
                  matchLabels:
                    additionalProperties:
                      type: string
                    description: |-
                      matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                      map is equivalent to an element of matchExpressions, whose key field is "key", the
                      operator is "In", and the values array contains only "value". The requirements are ANDed.
                    type: object
                type: object
                x-kubernetes-map-type: atomic
              preserveVlanInterfaces:
                description: Enable automatic detection and preservation of VLAN interfaces
                type: boolean
              vlanInterfaces:
                description: Optional explicit list of VLAN interface names to preserve
                  (e.g., eth0.10, bond0.20)
                items:
                  type: string
                type: array
            required:
            - defaultInterface
            type: object
          status:
            properties:
              conditions:
                description: |-
                  Conditions represents the latest state of the object
                  Conditions of nodes in the provider network
                items:
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    node:
                      description: Node name
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              notReadyNodes:
                description: Nodes that are not ready
                items:
                  type: string
                type: array
              ready:
                description: Whether the provider network is ready
                type: boolean
              readyNodes:
                description: Nodes that are ready
                items:
                  type: string
                type: array
              vlans:
                description: VLANs in use by this provider network
                items:
                  type: string
                type: array
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: qos-policies.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: QoSPolicy
    listKind: QoSPolicyList
    plural: qos-policies
    shortNames:
    - qos
    singular: qos-policy
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.shared
      name: Shared
      type: string
    - jsonPath: .spec.bindingType
      name: BindingType
      type: string
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              bandwidthLimitRules:
                description: Bandwidth limit rules to apply
                items:
                  description: BandwidthLimitRule describes the rule of a bandwidth
                    limit.
                  properties:
                    burstMax:
                      description: Maximum burst in MB (e.g., 10 or 0.5 for 500KB)
                      type: string
                    direction:
                      description: Traffic direction (ingress/egress)
                      type: string
                    interface:
                      description: Interface name
                      type: string
                    matchType:
                      description: Match type
                      type: string
                    matchValue:
                      description: Match value
                      type: string
                    name:
                      description: Rule name
                      type: string
                    priority:
                      description: Rule priority
                      type: integer
                    rateMax:
                      description: Maximum rate in Mbps (e.g., 100 or 0.5 for 500Kbps)
                      type: string
                  required:
                  - name
                  type: object
                type: array
              bindingType:
                description: Binding type (e.g., pod, namespace)
                type: string
              shared:
                description: Whether the QoS policy is shared across multiple pods
                type: boolean
            type: object
          status:
            properties:
              bandwidthLimitRules:
                description: Active bandwidth limit rules
                items:
                  description: BandwidthLimitRule describes the rule of a bandwidth
                    limit.
                  properties:
                    burstMax:
                      description: Maximum burst in MB (e.g., 10 or 0.5 for 500KB)
                      type: string
                    direction:
                      description: Traffic direction (ingress/egress)
                      type: string
                    interface:
                      description: Interface name
                      type: string
                    matchType:
                      description: Match type
                      type: string
                    matchValue:
                      description: Match value
                      type: string
                    name:
                      description: Rule name
                      type: string
                    priority:
                      description: Rule priority
                      type: integer
                    rateMax:
                      description: Maximum rate in Mbps (e.g., 100 or 0.5 for 500Kbps)
                      type: string
                  required:
                  - name
                  type: object
                type: array
              bindingType:
                description: Binding type of the QoS policy
                type: string
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              shared:
                description: Whether the QoS policy is shared
                type: boolean
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: security-groups.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: SecurityGroup
    listKind: SecurityGroupList
    plural: security-groups
    shortNames:
    - sg
    singular: security-group
  scope: Cluster
  versions:
  - name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              allowSameGroupTraffic:
                description: Allow traffic between pods in the same security group
                type: boolean
              egressRules:
                description: Egress traffic rules for the security group
                items:
                  properties:
                    ipVersion:
                      description: IP version (IPv4 or IPv6)
                      type: string
                    localAddress:
                      description: Local address or CIDR
                      type: string
                    policy:
                      description: Policy action (allow, pass or deny)
                      type: string
                    portRangeMax:
                      description: End of port range (1-65535)
                      type: integer
                    portRangeMin:
                      description: Start of port range (1-65535)
                      type: integer
                    priority:
                      description: Rule priority (1-16384)
                      type: integer
                    protocol:
                      description: Protocol (tcp, udp, icmp, or all)
                      type: string
                    remoteAddress:
                      description: Remote address or CIDR
                      type: string
                    remoteSecurityGroup:
                      description: Remote security group name
                      type: string
                    remoteType:
                      description: Type of remote (address, cidr, or securityGroup)
                      type: string
                    sourcePortRangeMax:
                      description: End of source port range (1-65535)
                      type: integer
                    sourcePortRangeMin:
                      description: Start of source port range (1-65535)
                      type: integer
                  type: object
                type: array
              ingressRules:
                description: Ingress traffic rules for the security group
                items:
                  properties:
                    ipVersion:
                      description: IP version (IPv4 or IPv6)
                      type: string
                    localAddress:
                      description: Local address or CIDR
                      type: string
                    policy:
                      description: Policy action (allow, pass or deny)
                      type: string
                    portRangeMax:
                      description: End of port range (1-65535)
                      type: integer
                    portRangeMin:
                      description: Start of port range (1-65535)
                      type: integer
                    priority:
                      description: Rule priority (1-16384)
                      type: integer
                    protocol:
                      description: Protocol (tcp, udp, icmp, or all)
                      type: string
                    remoteAddress:
                      description: Remote address or CIDR
                      type: string
                    remoteSecurityGroup:
                      description: Remote security group name
                      type: string
                    remoteType:
                      description: Type of remote (address, cidr, or securityGroup)
                      type: string
                    sourcePortRangeMax:
                      description: End of source port range (1-65535)
                      type: integer
                    sourcePortRangeMin:
                      description: Start of source port range (1-65535)
                      type: integer
                  type: object
                type: array
              tier:
                description: ACL tier to which the rules are added
                type: integer
            type: object
          status:
            properties:
              allowSameGroupTraffic:
                description: Current allow same group traffic setting
                type: boolean
              egressLastSyncSuccess:
                description: Last egress sync success status
                type: boolean
              egressMd5:
                description: MD5 hash of egress rules
                type: string
              ingressLastSyncSuccess:
                description: Last ingress sync success status
                type: boolean
              ingressMd5:
                description: MD5 hash of ingress rules
                type: string
              portGroup:
                description: OVN port group name
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: subnets.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: Subnet
    listKind: SubnetList
    plural: subnets
    shortNames:
    - subnet
    singular: subnet
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.provider
      name: Provider
      type: string
    - jsonPath: .spec.vpc
      name: Vpc
      type: string
    - jsonPath: .spec.vlan
      name: Vlan
      type: string
    - jsonPath: .spec.protocol
      name: Protocol
      type: string
    - jsonPath: .spec.cidrBlock
      name: CIDR
      type: string
    - jsonPath: .spec.private
      name: Private
      type: boolean
    - jsonPath: .spec.natOutgoing
      name: NAT
      type: boolean
    - jsonPath: .spec.default
      name: Default
      type: boolean
    - jsonPath: .spec.gatewayType
      name: GatewayType
      type: string
    - jsonPath: .status.v4usingIPs
      name: V4Used
      type: number
    - jsonPath: .status.v4availableIPs
      name: V4Available
      type: number
    - jsonPath: .status.v6usingIPs
      name: V6Used
      type: number
    - jsonPath: .status.v6availableIPs
      name: V6Available
      type: number
    - jsonPath: .spec.excludeIps
      name: ExcludeIPs
      type: string
    - jsonPath: .status.u2oInterconnectionIP
      name: U2OInterconnectionIP
      type: string
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              acls:
                description: ACL rules for the subnet.
                items:
                  properties:
                    action:
                      type: string
                    direction:
                      type: string
                    match:
                      type: string
                    priority:
                      type: integer
                  type: object
                type: array
              allowEWTraffic:
                description: Allow east-west traffic across subnets.
                type: boolean
              allowSubnets:
                description: Allowed subnets for east-west traffic.
                items:
                  type: string
                type: array
              cidrBlock:
                description: CIDR block for the subnet. Immutable after creation.
                type: string
              default:
                description: Whether this is the default subnet.
                type: boolean
              dhcpV4Options:
                description: DHCPv4 options UUID.
                type: string
              dhcpV6Options:
                description: DHCPv6 options UUID.
                type: string
              disableGatewayCheck:
                description: Disable gateway readiness check.
                type: boolean
              disableInterConnection:
                description: Disable interconnection for the subnet.
                type: boolean
              enableDHCP:
                description: Enable DHCP for the subnet.
                type: boolean
              enableEcmp:
                description: Enable ECMP for centralized gateway.
                type: boolean
              enableExternalLBAddress:
                description: Enable external LB address support.
                type: boolean
              enableIPv6RA:
                description: Enable IPv6 Router Advertisement.
                type: boolean
              enableLb:
                description: Enable LoadBalancer on this subnet.
                type: boolean
              enableMulticastSnoop:
                description: Enable multicast snoop.
                type: boolean
              excludeIps:
                description: IP addresses to exclude from allocation.
                items:
                  type: string
                type: array
              externalEgressGateway:
                description: External egress gateway IPs.
                type: string
              gateway:
                description: Gateway IP address for the subnet.
                type: string
              gatewayNode:
                description: Gateway node(s) for centralized gateway type.
                type: string
              gatewayNodeSelectors:
                description: Selectors to choose gateway nodes.
                items:
                  description: |-
                    A label selector is a label query over a set of resources. The result of matchLabels and
                    matchExpressions are ANDed. An empty label selector matches all objects. A null
                    label selector matches no objects.
                  properties:
                    matchExpressions:
                      description: matchExpressions is a list of label selector requirements.
                        The requirements are ANDed.
                      items:
                        description: |-
                          A label selector requirement is a selector that contains values, a key, and an operator that
                          relates the key and values.
                        properties:
                          key:
                            description: key is the label key that the selector applies
                              to.
                            type: string
                          operator:
                            description: |-
                              operator represents a key's relationship to a set of values.
                              Valid operators are In, NotIn, Exists and DoesNotExist.
                            type: string
                          values:
                            description: |-
                              values is an array of string values. If the operator is In or NotIn,
                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                              the values array must be empty. This array is replaced during a strategic
                              merge patch.
                            items:
                              type: string
                            type: array
                            x-kubernetes-list-type: atomic
                        required:
                        - key
                        - operator
                        type: object
                      type: array
                      x-kubernetes-list-type: atomic
                    matchLabels:
                      additionalProperties:
                        type: string
                      description: |-
                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                      type: object
                  type: object
                  x-kubernetes-map-type: atomic
                type: array
              gatewayType:
                description: Gateway type (distributed or centralized).
                type: string
              ipv6RAConfigs:
                description: IPv6 RA configuration options.
                type: string
              logicalGateway:
                description: Enable logical gateway.
                type: boolean
              mtu:
                description: MTU for pods in this subnet.
                format: int32
                type: integer
              namespaceSelectors:
                description: Namespace label selectors to associate with this subnet.
                items:
                  description: |-
                    A label selector is a label query over a set of resources. The result of matchLabels and
                    matchExpressions are ANDed. An empty label selector matches all objects. A null
                    label selector matches no objects.
                  properties:
                    matchExpressions:
                      description: matchExpressions is a list of label selector requirements.
                        The requirements are ANDed.
                      items:
                        description: |-
                          A label selector requirement is a selector that contains values, a key, and an operator that
                          relates the key and values.
                        properties:
                          key:
                            description: key is the label key that the selector applies
                              to.
                            type: string
                          operator:
                            description: |-
                              operator represents a key's relationship to a set of values.
                              Valid operators are In, NotIn, Exists and DoesNotExist.
                            type: string
                          values:
                            description: |-
                              values is an array of string values. If the operator is In or NotIn,
                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                              the values array must be empty. This array is replaced during a strategic
                              merge patch.
                            items:
                              type: string
                            type: array
                            x-kubernetes-list-type: atomic
                        required:
                        - key
                        - operator
                        type: object
                      type: array
                      x-kubernetes-list-type: atomic
                    matchLabels:
                      additionalProperties:
                        type: string
                      description: |-
                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                      type: object
                  type: object
                  x-kubernetes-map-type: atomic
                type: array
              namespaces:
                description: List of namespaces associated with this subnet.
                items:
                  type: string
                type: array
              natOutgoing:
                description: Enable NAT outgoing for the subnet.
                type: boolean
              natOutgoingPolicyRules:
                description: NAT outgoing policy rules.
                items:
                  properties:
                    action:
                      type: string
                    match:
                      properties:
                        dstIPs:
                          type: string
                        srcIPs:
                          type: string
                      type: object
                  type: object
                type: array
              nodeNetwork:
                description: Node network name for underlay.
                type: string
              policyRoutingPriority:
                description: Policy routing priority.
                format: int32
                type: integer
              policyRoutingTableID:
                description: Policy routing table ID.
                format: int32
                type: integer
              private:
                description: Whether the subnet is private.
                type: boolean
              protocol:
                description: Network protocol (IPv4, IPv6, or Dual). Immutable after
                  creation.
                type: string
              provider:
                description: Provider network name.
                type: string
              routeTable:
                description: Route table associated with the subnet.
                type: string
              u2oInterconnection:
                description: Enable underlay to overlay interconnection.
                type: boolean
              u2oInterconnectionIP:
                description: Underlay to overlay interconnection IP.
                type: string
              vips:
                description: Virtual IP addresses for the subnet.
                items:
                  type: string
                type: array
              vlan:
                description: VLAN ID or provider network name.
                type: string
              vpc:
                description: VPC name for the subnet. Immutable after creation.
                type: string
            type: object
          status:
            properties:
              activateGateway:
                type: string
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              dhcpV4OptionsUUID:
                type: string
              dhcpV6OptionsUUID:
                type: string
              mcastQuerierIP:
                type: string
              mcastQuerierMAC:
                type: string
              natOutgoingPolicyRules:
                description: NAT outgoing policy rules.
                items:
                  properties:
                    action:
                      type: string
                    match:
                      properties:
                        dstIPs:
                          type: string
                        srcIPs:
                          type: string
                      type: object
                    ruleID:
                      type: string
                  type: object
                type: array
              u2oInterconnectionIP:
                description: Underlay to overlay interconnection IP.
                type: string
              u2oInterconnectionMAC:
                type: string
              u2oInterconnectionVPC:
                type: string
              v4availableIPrange:
                type: string
              v4availableIPs:
                type: number
              v4usingIPrange:
                type: string
              v4usingIPs:
                type: number
              v6availableIPrange:
                type: string
              v6availableIPs:
                type: number
              v6usingIPrange:
                type: string
              v6usingIPs:
                type: number
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: switch-lb-rules.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: SwitchLBRule
    listKind: SwitchLBRuleList
    plural: switch-lb-rules
    shortNames:
    - slr
    singular: switch-lb-rule
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
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              endpoints:
                items:
                  type: string
                type: array
              namespace:
                type: string
              ports:
                items:
                  properties:
                    name:
                      description: Port name
                      type: string
                    port:
                      description: Service port number (1-65535)
                      format: int32
                      type: integer
                    protocol:
                      description: Protocol (TCP or UDP)
                      type: string
                    targetPort:
                      description: Target port number (1-65535)
                      format: int32
                      type: integer
                  type: object
                type: array
              selector:
                items:
                  type: string
                type: array
              sessionAffinity:
                type: string
              vip:
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              ports:
                description: Configured ports
                type: string
              service:
                description: Associated service name
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: vips.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: Vip
    listKind: VipList
    plural: vips
    shortNames:
    - vip
    singular: vip
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.namespace
      name: Namespace
      type: string
    - jsonPath: .status.v4ip
      name: V4IP
      type: string
    - jsonPath: .status.v6ip
      name: V6IP
      type: string
    - jsonPath: .status.mac
      name: Mac
      type: string
    - jsonPath: .spec.subnet
      name: Subnet
      type: string
    - jsonPath: .status.type
      name: Type
      type: string
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              attachSubnets:
                description: Additional subnets to attach
                items:
                  type: string
                type: array
              macAddress:
                description: MAC address for the VIP
                type: string
              namespace:
                description: Namespace where the VIP is created. This field is immutable
                  after creation.
                type: string
              selector:
                description: Pod names to be selected by this VIP
                items:
                  type: string
                type: array
              subnet:
                description: Subnet name for the VIP. This field is immutable after
                  creation.
                type: string
              type:
                description: Type of VIP. This field is immutable after creation.
                type: string
              v4ip:
                description: 'usage type: switch lb vip, allowed address pair vip
                  by default'
                type: string
              v6ip:
                description: Specific IPv6 address to use (optional, will be allocated
                  if not specified)
                type: string
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              mac:
                description: MAC address associated with the VIP
                type: string
              selector:
                description: Pod names selected by this VIP
                items:
                  type: string
                type: array
              type:
                description: Type of VIP (e.g., Layer2, HealthCheck)
                type: string
              v4ip:
                description: Allocated IPv4 address
                type: string
              v6ip:
                description: Allocated IPv6 address
                type: string
            type: object
        type: object
    served: true
    storage: true
    subresources: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: vlans.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: Vlan
    listKind: VlanList
    plural: vlans
    shortNames:
    - vlan
    singular: vlan
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.id
      name: ID
      type: string
    - jsonPath: .spec.provider
      name: Provider
      type: string
    - jsonPath: .status.conflict
      name: conflict
      type: boolean
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              id:
                description: VLAN ID (0-4095). This field is immutable after creation.
                type: integer
              provider:
                description: Provider network name. This field is immutable after
                  creation.
                type: string
              providerInterfaceName:
                description: 'Deprecated: in favor of provider'
                type: string
              vlanId:
                description: deprecated fields, use ID & Provider instead
                type: integer
            required:
            - provider
            type: object
          status:
            properties:
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              conflict:
                description: Whether there is a conflict with this VLAN
                type: boolean
              subnets:
                description: List of subnet names using this VLAN
                items:
                  type: string
                type: array
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: vpc-dnses.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: VpcDns
    listKind: VpcDnsList
    plural: vpc-dnses
    shortNames:
    - vpc-dns
    singular: vpc-dns
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
    - jsonPath: .spec.corefile
      name: Corefile
      type: string
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              corefile:
                default: vpc-dns-corefile
                description: CoreDNS corefile configuration
                type: string
              replicas:
                description: Number of DNS server replicas (1-3)
                format: int32
                type: integer
              subnet:
                description: Subnet name for the DNS service. This field is immutable
                  after creation.
                type: string
              vpc:
                description: VPC name for the DNS service. This field is immutable
                  after creation.
                type: string
            type: object
          status:
            properties:
              active:
                description: Whether the VPC DNS service is active
                type: boolean
              conditions:
                description: Conditions represent the latest state of the VPC DNS
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: vpc-egress-gateways.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: VpcEgressGateway
    listKind: VpcEgressGatewayList
    plural: vpc-egress-gateways
    shortNames:
    - vpc-egress-gw
    - veg
    singular: vpc-egress-gateway
  scope: Namespaced
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.vpc
      name: Vpc
      type: string
    - jsonPath: .spec.replicas
      name: Replicas
      type: integer
    - jsonPath: .spec.bfd.enabled
      name: bfd
      type: boolean
    - jsonPath: .spec.externalSubnet
      name: External Subnet
      type: string
    - jsonPath: .status.phase
      name: Phase
      type: string
    - jsonPath: .status.ready
      name: Ready
      type: boolean
    - jsonPath: .status.internalIPs
      name: Internal IPs
      priority: 1
      type: string
    - jsonPath: .status.externalIPs
      name: External IPs
      priority: 1
      type: string
    - jsonPath: .status.workload.nodes
      name: Working Nodes
      priority: 1
      type: string
    - jsonPath: .metadata.creationTimestamp
      name: Age
      type: date
    name: v1
    schema:
      openAPIV3Schema:
        description: vpc egress gateway is used to forward the egress traffic from
          the VPC to the external network
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              bandwidth:
                description: |-
                  Optional bandwidth limit for each egress gateway instance in both ingress and egress directions.
                  If not specified, there will be no bandwidth limit.
                properties:
                  egress:
                    description: egress bandwidth limit in Mbps
                    format: int64
                    type: integer
                  ingress:
                    description: ingress bandwidth limit in Mbps
                    format: int64
                    type: integer
                type: object
              bfd:
                description: BFD configuration
                properties:
                  enabled:
                    default: false
                    description: |-
                      whether to enable BFD
                      if set to true, the egress gateway will establish BFD session(s) with the VPC BFD LRP
                      the VPC's .spec.bfd.enabled must be set to true to enable BFD
                    type: boolean
                  minRX:
                    default: 1000
                    description: optional BFD minRX/minTX/multiplier
                    format: int32
                    maximum: 3600000
                    minimum: 1
                    type: integer
                  minTX:
                    default: 1000
                    format: int32
                    maximum: 3600000
                    minimum: 1
                    type: integer
                  multiplier:
                    default: 3
                    format: int32
                    maximum: 3600000
                    minimum: 1
                    type: integer
                type: object
              bgpConf:
                description: |-
                  optional BGP configuration name
                  it references a cluster-scoped BgpConf resource
                type: string
              evpnConf:
                description: |-
                  optional EVPN configuration name
                  it references a cluster-scoped EvpnConf resource
                type: string
              externalIPs:
                description: External IP addresses for the egress gateway
                items:
                  type: string
                type: array
              externalSubnet:
                description: external subnet used to create the workload
                type: string
              image:
                description: |-
                  optional image used by the workload
                  if not specified, the default image passed in by kube-ovn-controller will be used
                type: string
              internalIPs:
                description: |-
                  optional internal/external IPs used to create the workload
                  these IPs must be in the internal/external subnet
                  the IPs count must NOT be less than the replicas count
                items:
                  type: string
                type: array
              internalSubnet:
                description: |-
                  optional internal subnet used to create the workload
                  if not specified, the workload will be created in the default subnet of the VPC
                type: string
              nodeSelector:
                description: optional node selector used to select the nodes where
                  the workload will be running
                items:
                  properties:
                    matchExpressions:
                      items:
                        description: |-
                          A node selector requirement is a selector that contains values, a key, and an operator
                          that relates the key and values.
                        properties:
                          key:
                            description: The label key that the selector applies to.
                            type: string
                          operator:
                            description: |-
                              Represents a key's relationship to a set of values.
                              Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                            type: string
                          values:
                            description: |-
                              An array of string values. If the operator is In or NotIn,
                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                              the values array must be empty. If the operator is Gt or Lt, the values
                              array must have a single element, which will be interpreted as an integer.
                              This array is replaced during a strategic merge patch.
                            items:
                              type: string
                            type: array
                            x-kubernetes-list-type: atomic
                        required:
                        - key
                        - operator
                        type: object
                      type: array
                    matchFields:
                      items:
                        description: |-
                          A node selector requirement is a selector that contains values, a key, and an operator
                          that relates the key and values.
                        properties:
                          key:
                            description: The label key that the selector applies to.
                            type: string
                          operator:
                            description: |-
                              Represents a key's relationship to a set of values.
                              Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                            type: string
                          values:
                            description: |-
                              An array of string values. If the operator is In or NotIn,
                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                              the values array must be empty. If the operator is Gt or Lt, the values
                              array must have a single element, which will be interpreted as an integer.
                              This array is replaced during a strategic merge patch.
                            items:
                              type: string
                            type: array
                            x-kubernetes-list-type: atomic
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
                type: array
              policies:
                description: |-
                  egress policies
                  at least one policy must be specified
                items:
                  properties:
                    ipBlocks:
                      description: CIDRs/subnets targeted by the egress traffic policy
                      items:
                        type: string
                      type: array
                    snat:
                      default: false
                      description: whether to enable SNAT/MASQUERADE for the egress
                        traffic
                      type: boolean
                    subnets:
                      items:
                        type: string
                      type: array
                  type: object
                type: array
              prefix:
                description: |-
                  optional name prefix used to generate the workload
                  the workload name will be generated as <prefix><vpc-egress-gateway-name>
                type: string
              replicas:
                default: 1
                description: workload replicas
                format: int32
                type: integer
              resources:
                description: |-
                  Compute Resources required for the container. If not specified, the controller will set a default value.
                  If specified, the controller will not set any default value and use the specified value directly.
                properties:
                  claims:
                    description: |-
                      Claims lists the names of resources, defined in spec.resourceClaims,
                      that are used by this container.

                      This field depends on the
                      DynamicResourceAllocation feature gate.

                      This field is immutable. It can only be set for containers.
                    items:
                      description: ResourceClaim references one entry in PodSpec.ResourceClaims.
                      properties:
                        name:
                          description: |-
                            Name must match the name of one entry in pod.spec.resourceClaims of
                            the Pod where this field is used. It makes that resource available
                            inside a container.
                          type: string
                        request:
                          description: |-
                            Request is the name chosen for a request in the referenced claim.
                            If empty, everything from the claim is made available, otherwise
                            only the result of this request.
                          type: string
                      required:
                      - name
                      type: object
                    type: array
                    x-kubernetes-list-map-keys:
                    - name
                    x-kubernetes-list-type: map
                  limits:
                    additionalProperties:
                      anyOf:
                      - type: integer
                      - type: string
                      pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                      x-kubernetes-int-or-string: true
                    description: |-
                      Limits describes the maximum amount of compute resources allowed.
                      More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
                    type: object
                  requests:
                    additionalProperties:
                      anyOf:
                      - type: integer
                      - type: string
                      pattern: ^(\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))(([KMGTPE]i)|[numkMGTPE]|([eE](\+|-)?(([0-9]+(\.[0-9]*)?)|(\.[0-9]+))))?$
                      x-kubernetes-int-or-string: true
                    description: |-
                      Requests describes the minimum amount of compute resources required.
                      If Requests is omitted for a container, it defaults to Limits if that is explicitly specified,
                      otherwise to an implementation-defined value. Requests cannot exceed Limits.
                      More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
                    type: object
                type: object
              selectors:
                description: namespace/pod selectors
                items:
                  properties:
                    namespaceSelector:
                      description: |-
                        A label selector is a label query over a set of resources. The result of matchLabels and
                        matchExpressions are ANDed. An empty label selector matches all objects. A null
                        label selector matches no objects.
                      properties:
                        matchExpressions:
                          description: matchExpressions is a list of label selector
                            requirements. The requirements are ANDed.
                          items:
                            description: |-
                              A label selector requirement is a selector that contains values, a key, and an operator that
                              relates the key and values.
                            properties:
                              key:
                                description: key is the label key that the selector
                                  applies to.
                                type: string
                              operator:
                                description: |-
                                  operator represents a key's relationship to a set of values.
                                  Valid operators are In, NotIn, Exists and DoesNotExist.
                                type: string
                              values:
                                description: |-
                                  values is an array of string values. If the operator is In or NotIn,
                                  the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                  the values array must be empty. This array is replaced during a strategic
                                  merge patch.
                                items:
                                  type: string
                                type: array
                                x-kubernetes-list-type: atomic
                            required:
                            - key
                            - operator
                            type: object
                          type: array
                          x-kubernetes-list-type: atomic
                        matchLabels:
                          additionalProperties:
                            type: string
                          description: |-
                            matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                            map is equivalent to an element of matchExpressions, whose key field is "key", the
                            operator is "In", and the values array contains only "value". The requirements are ANDed.
                          type: object
                      type: object
                      x-kubernetes-map-type: atomic
                    podSelector:
                      description: |-
                        A label selector is a label query over a set of resources. The result of matchLabels and
                        matchExpressions are ANDed. An empty label selector matches all objects. A null
                        label selector matches no objects.
                      properties:
                        matchExpressions:
                          description: matchExpressions is a list of label selector
                            requirements. The requirements are ANDed.
                          items:
                            description: |-
                              A label selector requirement is a selector that contains values, a key, and an operator that
                              relates the key and values.
                            properties:
                              key:
                                description: key is the label key that the selector
                                  applies to.
                                type: string
                              operator:
                                description: |-
                                  operator represents a key's relationship to a set of values.
                                  Valid operators are In, NotIn, Exists and DoesNotExist.
                                type: string
                              values:
                                description: |-
                                  values is an array of string values. If the operator is In or NotIn,
                                  the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                  the values array must be empty. This array is replaced during a strategic
                                  merge patch.
                                items:
                                  type: string
                                type: array
                                x-kubernetes-list-type: atomic
                            required:
                            - key
                            - operator
                            type: object
                          type: array
                          x-kubernetes-list-type: atomic
                        matchLabels:
                          additionalProperties:
                            type: string
                          description: |-
                            matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                            map is equivalent to an element of matchExpressions, whose key field is "key", the
                            operator is "In", and the values array contains only "value". The requirements are ANDed.
                          type: object
                      type: object
                      x-kubernetes-map-type: atomic
                  type: object
                type: array
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
              trafficPolicy:
                default: Cluster
                description: |-
                  optional traffic policy used to control the traffic routing
                  if not specified, the default traffic policy "Cluster" will be used
                  if set to "Local", traffic will be routed to the gateway pod/instance on the same node when available
                  currently it works only for the default vpc
                type: string
              vpc:
                description: |-
                  optional VPC name
                  if not specified, the default VPC will be used
                type: string
            required:
            - externalSubnet
            type: object
          status:
            properties:
              conditions:
                description: Conditions represent the latest available observations
                  of the egress gateway's current state
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              externalIPs:
                description: External IP addresses assigned to the egress gateway
                items:
                  type: string
                type: array
              internalIPs:
                description: internal/external IPs used by the workload
                items:
                  type: string
                type: array
              labelSelector:
                description: Label selector for the egress gateway
                type: string
              phase:
                default: Pending
                description: Current phase of the egress gateway (Pending, Processing,
                  or Completed)
                type: string
              ready:
                default: false
                description: whether the egress gateway is ready
                type: boolean
              replicas:
                description: used by the scale subresource
                format: int32
                type: integer
              workload:
                description: workload information
                properties:
                  apiVersion:
                    type: string
                  kind:
                    type: string
                  name:
                    type: string
                  nodes:
                    description: nodes where the workload is running
                    items:
                      type: string
                    type: array
                type: object
            required:
            - conditions
            - phase
            type: object
        type: object
    served: true
    storage: true
    subresources:
      scale:
        labelSelectorPath: .status.labelSelector
        specReplicasPath: .spec.replicas
        statusReplicasPath: .status.replicas
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: vpc-nat-gateways.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: VpcNatGateway
    listKind: VpcNatGatewayList
    plural: vpc-nat-gateways
    shortNames:
    - vpc-nat-gw
    singular: vpc-nat-gateway
  scope: Cluster
  versions:
  - additionalPrinterColumns:
    - jsonPath: .spec.namespace
      name: Namespace
      type: string
    - jsonPath: .spec.vpc
      name: Vpc
      type: string
    - jsonPath: .spec.subnet
      name: Subnet
      type: string
    - jsonPath: .status.lanIp
      name: IPs
      type: string
    - jsonPath: .spec.replicas
      name: Replicas
      type: integer
    - jsonPath: .status.ready
      name: Ready
      type: boolean
    - jsonPath: .spec.bfd.enabled
      name: BFD
      type: boolean
    - jsonPath: .metadata.creationTimestamp
      name: Age
      type: date
    name: v1
    schema:
      openAPIV3Schema:
        description: |-
          VpcNatGateway represents a NAT gateway for a VPC, implemented as a StatefulSet Pod.

          Architecture note:
          The NAT gateway Pod does NOT support hot updates. Any changes to Spec fields (ExternalSubnets,
          Selector, Tolerations, Affinity, etc.) will trigger a StatefulSet template update,
          which causes the Pod to be recreated via RollingUpdate strategy. This is by design because:
           1. Network configuration (routes, iptables rules) is initialized at Pod startup
           2. Runtime state (vpc_cidrs, init status) is managed by separate handlers and will be
              automatically restored after Pod recreation through the normal reconciliation flow

          The only exception is QoSPolicy, which can be updated without Pod restart.
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              affinity:
                description: Affinity is a group of affinity scheduling rules.
                properties:
                  nodeAffinity:
                    description: Describes node affinity scheduling rules for the
                      pod.
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          The scheduler will prefer to schedule pods to nodes that satisfy
                          the affinity expressions specified by this field, but it may choose
                          a node that violates one or more of the expressions. The node that is
                          most preferred is the one with the greatest sum of weights, i.e.
                          for each node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling affinity expressions, etc.),
                          compute a sum by iterating through the elements of this field and adding
                          "weight" to the sum if the node matches the corresponding matchExpressions; the
                          node(s) with the highest sum are the most preferred.
                        items:
                          description: |-
                            An empty preferred scheduling term matches all objects with implicit weight 0
                            (i.e. it's a no-op). A null preferred scheduling term matches no objects (i.e. is also a no-op).
                          properties:
                            preference:
                              description: A node selector term, associated with the
                                corresponding weight.
                              properties:
                                matchExpressions:
                                  description: A list of node selector requirements
                                    by node's labels.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchFields:
                                  description: A list of node selector requirements
                                    by node's fields.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                              type: object
                              x-kubernetes-map-type: atomic
                            weight:
                              description: Weight associated with matching the corresponding
                                nodeSelectorTerm, in the range 1-100.
                              format: int32
                              type: integer
                          required:
                          - preference
                          - weight
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          If the affinity requirements specified by this field are not met at
                          scheduling time, the pod will not be scheduled onto the node.
                          If the affinity requirements specified by this field cease to be met
                          at some point during pod execution (e.g. due to an update), the system
                          may or may not try to eventually evict the pod from its node.
                        properties:
                          nodeSelectorTerms:
                            description: Required. A list of node selector terms.
                              The terms are ORed.
                            items:
                              description: |-
                                A null or empty node selector term matches no objects. The requirements of
                                them are ANDed.
                                The TopologySelectorTerm type implements a subset of the NodeSelectorTerm.
                              properties:
                                matchExpressions:
                                  description: A list of node selector requirements
                                    by node's labels.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchFields:
                                  description: A list of node selector requirements
                                    by node's fields.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                              type: object
                              x-kubernetes-map-type: atomic
                            type: array
                            x-kubernetes-list-type: atomic
                        required:
                        - nodeSelectorTerms
                        type: object
                        x-kubernetes-map-type: atomic
                    type: object
                  podAffinity:
                    description: Describes pod affinity scheduling rules (e.g. co-locate
                      this pod in the same node, zone, etc. as some other pod(s)).
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          The scheduler will prefer to schedule pods to nodes that satisfy
                          the affinity expressions specified by this field, but it may choose
                          a node that violates one or more of the expressions. The node that is
                          most preferred is the one with the greatest sum of weights, i.e.
                          for each node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling affinity expressions, etc.),
                          compute a sum by iterating through the elements of this field and adding
                          "weight" to the sum if the node has pods which matches the corresponding podAffinityTerm; the
                          node(s) with the highest sum are the most preferred.
                        items:
                          description: The weights of all of the matched WeightedPodAffinityTerm
                            fields are added per-node to find the most preferred node(s)
                          properties:
                            podAffinityTerm:
                              description: Required. A pod affinity term, associated
                                with the corresponding weight.
                              properties:
                                labelSelector:
                                  description: |-
                                    A label query over a set of resources, in this case pods.
                                    If it's null, this PodAffinityTerm matches with no Pods.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                matchLabelKeys:
                                  description: |-
                                    MatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                    Also, matchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                mismatchLabelKeys:
                                  description: |-
                                    MismatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                    Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                namespaceSelector:
                                  description: |-
                                    A label query over the set of namespaces that the term applies to.
                                    The term is applied to the union of the namespaces selected by this field
                                    and the ones listed in the namespaces field.
                                    null selector and null or empty namespaces list means "this pod's namespace".
                                    An empty selector ({}) matches all namespaces.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                namespaces:
                                  description: |-
                                    namespaces specifies a static list of namespace names that the term applies to.
                                    The term is applied to the union of the namespaces listed in this field
                                    and the ones selected by namespaceSelector.
                                    null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                topologyKey:
                                  description: |-
                                    This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                    the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                    whose value of the label with key topologyKey matches that of any node on which any of the
                                    selected pods is running.
                                    Empty topologyKey is not allowed.
                                  type: string
                              required:
                              - topologyKey
                              type: object
                            weight:
                              description: |-
                                weight associated with matching the corresponding podAffinityTerm,
                                in the range 1-100.
                              format: int32
                              type: integer
                          required:
                          - podAffinityTerm
                          - weight
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          If the affinity requirements specified by this field are not met at
                          scheduling time, the pod will not be scheduled onto the node.
                          If the affinity requirements specified by this field cease to be met
                          at some point during pod execution (e.g. due to a pod label update), the
                          system may or may not try to eventually evict the pod from its node.
                          When there are multiple elements, the lists of nodes corresponding to each
                          podAffinityTerm are intersected, i.e. all terms must be satisfied.
                        items:
                          description: |-
                            Defines a set of pods (namely those matching the labelSelector
                            relative to the given namespace(s)) that this pod should be
                            co-located (affinity) or not co-located (anti-affinity) with,
                            where co-located is defined as running on a node whose value of
                            the label with key <topologyKey> matches that of any node on which
                            a pod of the set of pods is running
                          properties:
                            labelSelector:
                              description: |-
                                A label query over a set of resources, in this case pods.
                                If it's null, this PodAffinityTerm matches with no Pods.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            matchLabelKeys:
                              description: |-
                                MatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                Also, matchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            mismatchLabelKeys:
                              description: |-
                                MismatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            namespaceSelector:
                              description: |-
                                A label query over the set of namespaces that the term applies to.
                                The term is applied to the union of the namespaces selected by this field
                                and the ones listed in the namespaces field.
                                null selector and null or empty namespaces list means "this pod's namespace".
                                An empty selector ({}) matches all namespaces.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            namespaces:
                              description: |-
                                namespaces specifies a static list of namespace names that the term applies to.
                                The term is applied to the union of the namespaces listed in this field
                                and the ones selected by namespaceSelector.
                                null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            topologyKey:
                              description: |-
                                This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                whose value of the label with key topologyKey matches that of any node on which any of the
                                selected pods is running.
                                Empty topologyKey is not allowed.
                              type: string
                          required:
                          - topologyKey
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                    type: object
                  podAntiAffinity:
                    description: Describes pod anti-affinity scheduling rules (e.g.
                      avoid putting this pod in the same node, zone, etc. as some
                      other pod(s)).
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          The scheduler will prefer to schedule pods to nodes that satisfy
                          the anti-affinity expressions specified by this field, but it may choose
                          a node that violates one or more of the expressions. The node that is
                          most preferred is the one with the greatest sum of weights, i.e.
                          for each node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling anti-affinity expressions, etc.),
                          compute a sum by iterating through the elements of this field and subtracting
                          "weight" from the sum if the node has pods which matches the corresponding podAffinityTerm; the
                          node(s) with the highest sum are the most preferred.
                        items:
                          description: The weights of all of the matched WeightedPodAffinityTerm
                            fields are added per-node to find the most preferred node(s)
                          properties:
                            podAffinityTerm:
                              description: Required. A pod affinity term, associated
                                with the corresponding weight.
                              properties:
                                labelSelector:
                                  description: |-
                                    A label query over a set of resources, in this case pods.
                                    If it's null, this PodAffinityTerm matches with no Pods.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                matchLabelKeys:
                                  description: |-
                                    MatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                    Also, matchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                mismatchLabelKeys:
                                  description: |-
                                    MismatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                    Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                namespaceSelector:
                                  description: |-
                                    A label query over the set of namespaces that the term applies to.
                                    The term is applied to the union of the namespaces selected by this field
                                    and the ones listed in the namespaces field.
                                    null selector and null or empty namespaces list means "this pod's namespace".
                                    An empty selector ({}) matches all namespaces.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                namespaces:
                                  description: |-
                                    namespaces specifies a static list of namespace names that the term applies to.
                                    The term is applied to the union of the namespaces listed in this field
                                    and the ones selected by namespaceSelector.
                                    null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                topologyKey:
                                  description: |-
                                    This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                    the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                    whose value of the label with key topologyKey matches that of any node on which any of the
                                    selected pods is running.
                                    Empty topologyKey is not allowed.
                                  type: string
                              required:
                              - topologyKey
                              type: object
                            weight:
                              description: |-
                                weight associated with matching the corresponding podAffinityTerm,
                                in the range 1-100.
                              format: int32
                              type: integer
                          required:
                          - podAffinityTerm
                          - weight
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          If the anti-affinity requirements specified by this field are not met at
                          scheduling time, the pod will not be scheduled onto the node.
                          If the anti-affinity requirements specified by this field cease to be met
                          at some point during pod execution (e.g. due to a pod label update), the
                          system may or may not try to eventually evict the pod from its node.
                          When there are multiple elements, the lists of nodes corresponding to each
                          podAffinityTerm are intersected, i.e. all terms must be satisfied.
                        items:
                          description: |-
                            Defines a set of pods (namely those matching the labelSelector
                            relative to the given namespace(s)) that this pod should be
                            co-located (affinity) or not co-located (anti-affinity) with,
                            where co-located is defined as running on a node whose value of
                            the label with key <topologyKey> matches that of any node on which
                            a pod of the set of pods is running
                          properties:
                            labelSelector:
                              description: |-
                                A label query over a set of resources, in this case pods.
                                If it's null, this PodAffinityTerm matches with no Pods.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            matchLabelKeys:
                              description: |-
                                MatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                Also, matchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            mismatchLabelKeys:
                              description: |-
                                MismatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            namespaceSelector:
                              description: |-
                                A label query over the set of namespaces that the term applies to.
                                The term is applied to the union of the namespaces selected by this field
                                and the ones listed in the namespaces field.
                                null selector and null or empty namespaces list means "this pod's namespace".
                                An empty selector ({}) matches all namespaces.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            namespaces:
                              description: |-
                                namespaces specifies a static list of namespace names that the term applies to.
                                The term is applied to the union of the namespaces listed in this field
                                and the ones selected by namespaceSelector.
                                null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            topologyKey:
                              description: |-
                                This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                whose value of the label with key topologyKey matches that of any node on which any of the
                                selected pods is running.
                                Empty topologyKey is not allowed.
                              type: string
                          required:
                          - topologyKey
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                    type: object
                type: object
              annotations:
                additionalProperties:
                  type: string
                description: |-
                  User-defined annotations for the StatefulSet NAT gateway Pod template.
                  Only effective at creation time; updates to this field are not detected.
                type: object
              bfd:
                description: BFD configuration for health monitoring and automatic
                  failover (HA mode only)
                properties:
                  enabled:
                    default: false
                    description: |-
                      Enable BFD health monitoring
                      When enabled, each gateway instance establishes a BFD session with the VPC's BFD port.
                      The VPC's spec.bfd.enabled must also be set to true.
                    type: boolean
                  minRX:
                    default: 1000
                    description: |-
                      BFD minimum receive interval in milliseconds
                      This is the minimum interval at which this gateway expects to receive BFD control packets.
                    format: int32
                    maximum: 3600000
                    minimum: 1
                    type: integer
                  minTX:
                    default: 1000
                    description: |-
                      BFD minimum transmit interval in milliseconds
                      This is the minimum interval at which this gateway will send BFD control packets.
                    format: int32
                    maximum: 3600000
                    minimum: 1
                    type: integer
                  multiplier:
                    default: 3
                    description: |-
                      BFD detection multiplier
                      Number of missed BFD packets before declaring the session down.
                      Detection time = MinRX * Multiplier
                    format: int32
                    maximum: 255
                    minimum: 1
                    type: integer
                type: object
              bgpSpeaker:
                description: BGP speaker configuration
                properties:
                  asn:
                    description: BGP ASN
                    format: int32
                    type: integer
                  enableGracefulRestart:
                    description: Enable graceful restart
                    type: boolean
                  enabled:
                    description: Whether to enable BGP speaker
                    type: boolean
                  extraArgs:
                    description: Extra arguments for BGP speaker
                    items:
                      type: string
                    type: array
                  holdTime:
                    description: BGP hold time
                    type: string
                  neighbors:
                    description: BGP neighbors
                    items:
                      type: string
                    type: array
                  password:
                    description: BGP password
                    type: string
                  remoteAsn:
                    description: BGP remote ASN
                    format: int32
                    type: integer
                  routerId:
                    description: BGP router ID
                    type: string
                type: object
              externalSubnets:
                description: External subnets accessible through the NAT gateway
                items:
                  type: string
                type: array
              internalCIDRs:
                description: |-
                  Internal CIDRs for OVN route injection.
                  Traffic from these CIDRs destined for 0.0.0.0/0 or ::/0 will be routed to NAT gateway instances.
                  This field is cumulative with internalSubnets.
                items:
                  type: string
                type: array
              internalSubnets:
                description: |-
                  Internal subnets by name (resolved to CIDRs) for OVN route injection.
                  Traffic from these subnets destined for 0.0.0.0/0 or ::/0 will be routed to NAT gateway instances.
                  This field is cumulative with internalCIDRs.
                items:
                  type: string
                type: array
              lanIp:
                description: |-
                  LAN IP address for the NAT gateway. This field is immutable after creation.
                  Used only when Replicas = 1 (non-HA mode).
                type: string
              namespace:
                description: |-
                  Namespace where the NAT gateway StatefulSet/Pod will be created.
                  If empty, defaults to the kube-ovn controller's own namespace (typically kube-system).
                type: string
              noDefaultEIP:
                description: Disable default EIP assignment
                type: boolean
              qosPolicy:
                description: QoS policy name to apply to the NAT gateway
                type: string
              replicas:
                default: 1
                description: |-
                  Number of gateway replicas for HA support.
                  When > 1, uses Deployment workload with pod anti-affinity to distribute instances across nodes.
                  When = 1 or unset, uses StatefulSet workload (legacy mode) for backward compatibility.
                format: int32
                minimum: 1
                type: integer
              routes:
                description: Static routes for the NAT gateway
                items:
                  properties:
                    cidr:
                      description: Route CIDR
                      type: string
                    nextHopIP:
                      description: Next hop IP
                      type: string
                  type: object
                type: array
              selector:
                description: Pod selector for the NAT gateway
                items:
                  type: string
                type: array
              subnet:
                description: Subnet name for the NAT gateway. This field is immutable
                  after creation.
                type: string
              tolerations:
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
              vpc:
                description: VPC name for the NAT gateway. This field is immutable
                  after creation.
                type: string
            type: object
          status:
            properties:
              affinity:
                description: Affinity is a group of affinity scheduling rules.
                properties:
                  nodeAffinity:
                    description: Describes node affinity scheduling rules for the
                      pod.
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          The scheduler will prefer to schedule pods to nodes that satisfy
                          the affinity expressions specified by this field, but it may choose
                          a node that violates one or more of the expressions. The node that is
                          most preferred is the one with the greatest sum of weights, i.e.
                          for each node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling affinity expressions, etc.),
                          compute a sum by iterating through the elements of this field and adding
                          "weight" to the sum if the node matches the corresponding matchExpressions; the
                          node(s) with the highest sum are the most preferred.
                        items:
                          description: |-
                            An empty preferred scheduling term matches all objects with implicit weight 0
                            (i.e. it's a no-op). A null preferred scheduling term matches no objects (i.e. is also a no-op).
                          properties:
                            preference:
                              description: A node selector term, associated with the
                                corresponding weight.
                              properties:
                                matchExpressions:
                                  description: A list of node selector requirements
                                    by node's labels.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchFields:
                                  description: A list of node selector requirements
                                    by node's fields.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                              type: object
                              x-kubernetes-map-type: atomic
                            weight:
                              description: Weight associated with matching the corresponding
                                nodeSelectorTerm, in the range 1-100.
                              format: int32
                              type: integer
                          required:
                          - preference
                          - weight
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          If the affinity requirements specified by this field are not met at
                          scheduling time, the pod will not be scheduled onto the node.
                          If the affinity requirements specified by this field cease to be met
                          at some point during pod execution (e.g. due to an update), the system
                          may or may not try to eventually evict the pod from its node.
                        properties:
                          nodeSelectorTerms:
                            description: Required. A list of node selector terms.
                              The terms are ORed.
                            items:
                              description: |-
                                A null or empty node selector term matches no objects. The requirements of
                                them are ANDed.
                                The TopologySelectorTerm type implements a subset of the NodeSelectorTerm.
                              properties:
                                matchExpressions:
                                  description: A list of node selector requirements
                                    by node's labels.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchFields:
                                  description: A list of node selector requirements
                                    by node's fields.
                                  items:
                                    description: |-
                                      A node selector requirement is a selector that contains values, a key, and an operator
                                      that relates the key and values.
                                    properties:
                                      key:
                                        description: The label key that the selector
                                          applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          Represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists, DoesNotExist. Gt, and Lt.
                                        type: string
                                      values:
                                        description: |-
                                          An array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. If the operator is Gt or Lt, the values
                                          array must have a single element, which will be interpreted as an integer.
                                          This array is replaced during a strategic merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                              type: object
                              x-kubernetes-map-type: atomic
                            type: array
                            x-kubernetes-list-type: atomic
                        required:
                        - nodeSelectorTerms
                        type: object
                        x-kubernetes-map-type: atomic
                    type: object
                  podAffinity:
                    description: Describes pod affinity scheduling rules (e.g. co-locate
                      this pod in the same node, zone, etc. as some other pod(s)).
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          The scheduler will prefer to schedule pods to nodes that satisfy
                          the affinity expressions specified by this field, but it may choose
                          a node that violates one or more of the expressions. The node that is
                          most preferred is the one with the greatest sum of weights, i.e.
                          for each node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling affinity expressions, etc.),
                          compute a sum by iterating through the elements of this field and adding
                          "weight" to the sum if the node has pods which matches the corresponding podAffinityTerm; the
                          node(s) with the highest sum are the most preferred.
                        items:
                          description: The weights of all of the matched WeightedPodAffinityTerm
                            fields are added per-node to find the most preferred node(s)
                          properties:
                            podAffinityTerm:
                              description: Required. A pod affinity term, associated
                                with the corresponding weight.
                              properties:
                                labelSelector:
                                  description: |-
                                    A label query over a set of resources, in this case pods.
                                    If it's null, this PodAffinityTerm matches with no Pods.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                matchLabelKeys:
                                  description: |-
                                    MatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                    Also, matchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                mismatchLabelKeys:
                                  description: |-
                                    MismatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                    Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                namespaceSelector:
                                  description: |-
                                    A label query over the set of namespaces that the term applies to.
                                    The term is applied to the union of the namespaces selected by this field
                                    and the ones listed in the namespaces field.
                                    null selector and null or empty namespaces list means "this pod's namespace".
                                    An empty selector ({}) matches all namespaces.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                namespaces:
                                  description: |-
                                    namespaces specifies a static list of namespace names that the term applies to.
                                    The term is applied to the union of the namespaces listed in this field
                                    and the ones selected by namespaceSelector.
                                    null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                topologyKey:
                                  description: |-
                                    This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                    the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                    whose value of the label with key topologyKey matches that of any node on which any of the
                                    selected pods is running.
                                    Empty topologyKey is not allowed.
                                  type: string
                              required:
                              - topologyKey
                              type: object
                            weight:
                              description: |-
                                weight associated with matching the corresponding podAffinityTerm,
                                in the range 1-100.
                              format: int32
                              type: integer
                          required:
                          - podAffinityTerm
                          - weight
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          If the affinity requirements specified by this field are not met at
                          scheduling time, the pod will not be scheduled onto the node.
                          If the affinity requirements specified by this field cease to be met
                          at some point during pod execution (e.g. due to a pod label update), the
                          system may or may not try to eventually evict the pod from its node.
                          When there are multiple elements, the lists of nodes corresponding to each
                          podAffinityTerm are intersected, i.e. all terms must be satisfied.
                        items:
                          description: |-
                            Defines a set of pods (namely those matching the labelSelector
                            relative to the given namespace(s)) that this pod should be
                            co-located (affinity) or not co-located (anti-affinity) with,
                            where co-located is defined as running on a node whose value of
                            the label with key <topologyKey> matches that of any node on which
                            a pod of the set of pods is running
                          properties:
                            labelSelector:
                              description: |-
                                A label query over a set of resources, in this case pods.
                                If it's null, this PodAffinityTerm matches with no Pods.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            matchLabelKeys:
                              description: |-
                                MatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                Also, matchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            mismatchLabelKeys:
                              description: |-
                                MismatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            namespaceSelector:
                              description: |-
                                A label query over the set of namespaces that the term applies to.
                                The term is applied to the union of the namespaces selected by this field
                                and the ones listed in the namespaces field.
                                null selector and null or empty namespaces list means "this pod's namespace".
                                An empty selector ({}) matches all namespaces.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            namespaces:
                              description: |-
                                namespaces specifies a static list of namespace names that the term applies to.
                                The term is applied to the union of the namespaces listed in this field
                                and the ones selected by namespaceSelector.
                                null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            topologyKey:
                              description: |-
                                This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                whose value of the label with key topologyKey matches that of any node on which any of the
                                selected pods is running.
                                Empty topologyKey is not allowed.
                              type: string
                          required:
                          - topologyKey
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                    type: object
                  podAntiAffinity:
                    description: Describes pod anti-affinity scheduling rules (e.g.
                      avoid putting this pod in the same node, zone, etc. as some
                      other pod(s)).
                    properties:
                      preferredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          The scheduler will prefer to schedule pods to nodes that satisfy
                          the anti-affinity expressions specified by this field, but it may choose
                          a node that violates one or more of the expressions. The node that is
                          most preferred is the one with the greatest sum of weights, i.e.
                          for each node that meets all of the scheduling requirements (resource
                          request, requiredDuringScheduling anti-affinity expressions, etc.),
                          compute a sum by iterating through the elements of this field and subtracting
                          "weight" from the sum if the node has pods which matches the corresponding podAffinityTerm; the
                          node(s) with the highest sum are the most preferred.
                        items:
                          description: The weights of all of the matched WeightedPodAffinityTerm
                            fields are added per-node to find the most preferred node(s)
                          properties:
                            podAffinityTerm:
                              description: Required. A pod affinity term, associated
                                with the corresponding weight.
                              properties:
                                labelSelector:
                                  description: |-
                                    A label query over a set of resources, in this case pods.
                                    If it's null, this PodAffinityTerm matches with no Pods.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                matchLabelKeys:
                                  description: |-
                                    MatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                    Also, matchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                mismatchLabelKeys:
                                  description: |-
                                    MismatchLabelKeys is a set of pod label keys to select which pods will
                                    be taken into consideration. The keys are used to lookup values from the
                                    incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                    to select the group of existing pods which pods will be taken into consideration
                                    for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                    pod labels will be ignored. The default value is empty.
                                    The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                    Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                namespaceSelector:
                                  description: |-
                                    A label query over the set of namespaces that the term applies to.
                                    The term is applied to the union of the namespaces selected by this field
                                    and the ones listed in the namespaces field.
                                    null selector and null or empty namespaces list means "this pod's namespace".
                                    An empty selector ({}) matches all namespaces.
                                  properties:
                                    matchExpressions:
                                      description: matchExpressions is a list of label
                                        selector requirements. The requirements are
                                        ANDed.
                                      items:
                                        description: |-
                                          A label selector requirement is a selector that contains values, a key, and an operator that
                                          relates the key and values.
                                        properties:
                                          key:
                                            description: key is the label key that
                                              the selector applies to.
                                            type: string
                                          operator:
                                            description: |-
                                              operator represents a key's relationship to a set of values.
                                              Valid operators are In, NotIn, Exists and DoesNotExist.
                                            type: string
                                          values:
                                            description: |-
                                              values is an array of string values. If the operator is In or NotIn,
                                              the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                              the values array must be empty. This array is replaced during a strategic
                                              merge patch.
                                            items:
                                              type: string
                                            type: array
                                            x-kubernetes-list-type: atomic
                                        required:
                                        - key
                                        - operator
                                        type: object
                                      type: array
                                      x-kubernetes-list-type: atomic
                                    matchLabels:
                                      additionalProperties:
                                        type: string
                                      description: |-
                                        matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                        map is equivalent to an element of matchExpressions, whose key field is "key", the
                                        operator is "In", and the values array contains only "value". The requirements are ANDed.
                                      type: object
                                  type: object
                                  x-kubernetes-map-type: atomic
                                namespaces:
                                  description: |-
                                    namespaces specifies a static list of namespace names that the term applies to.
                                    The term is applied to the union of the namespaces listed in this field
                                    and the ones selected by namespaceSelector.
                                    null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                                  items:
                                    type: string
                                  type: array
                                  x-kubernetes-list-type: atomic
                                topologyKey:
                                  description: |-
                                    This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                    the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                    whose value of the label with key topologyKey matches that of any node on which any of the
                                    selected pods is running.
                                    Empty topologyKey is not allowed.
                                  type: string
                              required:
                              - topologyKey
                              type: object
                            weight:
                              description: |-
                                weight associated with matching the corresponding podAffinityTerm,
                                in the range 1-100.
                              format: int32
                              type: integer
                          required:
                          - podAffinityTerm
                          - weight
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                      requiredDuringSchedulingIgnoredDuringExecution:
                        description: |-
                          If the anti-affinity requirements specified by this field are not met at
                          scheduling time, the pod will not be scheduled onto the node.
                          If the anti-affinity requirements specified by this field cease to be met
                          at some point during pod execution (e.g. due to a pod label update), the
                          system may or may not try to eventually evict the pod from its node.
                          When there are multiple elements, the lists of nodes corresponding to each
                          podAffinityTerm are intersected, i.e. all terms must be satisfied.
                        items:
                          description: |-
                            Defines a set of pods (namely those matching the labelSelector
                            relative to the given namespace(s)) that this pod should be
                            co-located (affinity) or not co-located (anti-affinity) with,
                            where co-located is defined as running on a node whose value of
                            the label with key <topologyKey> matches that of any node on which
                            a pod of the set of pods is running
                          properties:
                            labelSelector:
                              description: |-
                                A label query over a set of resources, in this case pods.
                                If it's null, this PodAffinityTerm matches with no Pods.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            matchLabelKeys:
                              description: |-
                                MatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key in (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both matchLabelKeys and labelSelector.
                                Also, matchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            mismatchLabelKeys:
                              description: |-
                                MismatchLabelKeys is a set of pod label keys to select which pods will
                                be taken into consideration. The keys are used to lookup values from the
                                incoming pod labels, those key-value labels are merged with `labelSelector` as `key notin (value)`
                                to select the group of existing pods which pods will be taken into consideration
                                for the incoming pod's pod (anti) affinity. Keys that don't exist in the incoming
                                pod labels will be ignored. The default value is empty.
                                The same key is forbidden to exist in both mismatchLabelKeys and labelSelector.
                                Also, mismatchLabelKeys cannot be set when labelSelector isn't set.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            namespaceSelector:
                              description: |-
                                A label query over the set of namespaces that the term applies to.
                                The term is applied to the union of the namespaces selected by this field
                                and the ones listed in the namespaces field.
                                null selector and null or empty namespaces list means "this pod's namespace".
                                An empty selector ({}) matches all namespaces.
                              properties:
                                matchExpressions:
                                  description: matchExpressions is a list of label
                                    selector requirements. The requirements are ANDed.
                                  items:
                                    description: |-
                                      A label selector requirement is a selector that contains values, a key, and an operator that
                                      relates the key and values.
                                    properties:
                                      key:
                                        description: key is the label key that the
                                          selector applies to.
                                        type: string
                                      operator:
                                        description: |-
                                          operator represents a key's relationship to a set of values.
                                          Valid operators are In, NotIn, Exists and DoesNotExist.
                                        type: string
                                      values:
                                        description: |-
                                          values is an array of string values. If the operator is In or NotIn,
                                          the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                          the values array must be empty. This array is replaced during a strategic
                                          merge patch.
                                        items:
                                          type: string
                                        type: array
                                        x-kubernetes-list-type: atomic
                                    required:
                                    - key
                                    - operator
                                    type: object
                                  type: array
                                  x-kubernetes-list-type: atomic
                                matchLabels:
                                  additionalProperties:
                                    type: string
                                  description: |-
                                    matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                                    map is equivalent to an element of matchExpressions, whose key field is "key", the
                                    operator is "In", and the values array contains only "value". The requirements are ANDed.
                                  type: object
                              type: object
                              x-kubernetes-map-type: atomic
                            namespaces:
                              description: |-
                                namespaces specifies a static list of namespace names that the term applies to.
                                The term is applied to the union of the namespaces listed in this field
                                and the ones selected by namespaceSelector.
                                null or empty namespaces list and null namespaceSelector means "this pod's namespace".
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                            topologyKey:
                              description: |-
                                This pod should be co-located (affinity) or not co-located (anti-affinity) with the pods matching
                                the labelSelector in the specified namespaces, where co-located is defined as running on a node
                                whose value of the label with key topologyKey matches that of any node on which any of the
                                selected pods is running.
                                Empty topologyKey is not allowed.
                              type: string
                          required:
                          - topologyKey
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                    type: object
                type: object
              externalSubnets:
                description: External subnets configured for the NAT gateway
                items:
                  type: string
                type: array
              internalCIDRs:
                description: Internal CIDRs configured for OVN route injection
                items:
                  type: string
                type: array
              internalSubnets:
                description: Internal subnets configured for OVN route injection
                items:
                  type: string
                type: array
              lanIp:
                description: |-
                  LAN IP address(es) for the NAT gateway.
                  For non-HA, this is the single LanIP from spec.
                  For HA, this is a comma-separated list of all IPs within the NAT gateway pods.
                type: string
              qosPolicy:
                description: QoS policy applied to the NAT gateway
                type: string
              ready:
                description: Ready state of the NAT gateway
                type: boolean
              replicas:
                description: Number of gateway replicas
                format: int32
                type: integer
              selector:
                description: Pod selector configured for the NAT gateway
                items:
                  type: string
                type: array
              tolerations:
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
              workload:
                description: Workload information (Deployment or StatefulSet)
                properties:
                  apiVersion:
                    description: API version of the workload (e.g., "apps/v1")
                    type: string
                  kind:
                    description: Kind of the workload ("Deployment" or "StatefulSet")
                    type: string
                  name:
                    description: Name of the workload
                    type: string
                  nodes:
                    description: Nodes where gateway instances are running
                    items:
                      type: string
                    type: array
                type: object
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
---
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  annotations:
    controller-gen.kubebuilder.io/version: v0.20.1
  name: vpcs.kubeovn.io
spec:
  group: kubeovn.io
  names:
    kind: Vpc
    listKind: VpcList
    plural: vpcs
    shortNames:
    - vpc
    singular: vpc
  scope: Cluster
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
    - jsonPath: .metadata.creationTimestamp
      name: Age
      type: date
    name: v1
    schema:
      openAPIV3Schema:
        properties:
          apiVersion:
            description: |-
              APIVersion defines the versioned schema of this representation of an object.
              Servers should convert recognized schemas to the latest internal value, and
              may reject unrecognized values.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#resources
            type: string
          kind:
            description: |-
              Kind is a string value representing the REST resource this object represents.
              Servers may infer this from the endpoint the client submits requests to.
              Cannot be updated.
              In CamelCase.
              More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#types-kinds
            type: string
          metadata:
            type: object
          spec:
            properties:
              bfdPort:
                description: |-
                  optional BFD LRP configuration
                  currently the LRP is used for vpc external gateway only
                properties:
                  enabled:
                    default: false
                    description: Enable BFD port
                    type: boolean
                  ip:
                    description: ip address(es) of the BFD port
                    type: string
                  nodeSelector:
                    description: |-
                      Optional node selector used to select the nodes where the BFD LRP will be hosted.
                      If not specified, at most 3 nodes will be selected.
                    properties:
                      matchExpressions:
                        description: matchExpressions is a list of label selector
                          requirements. The requirements are ANDed.
                        items:
                          description: |-
                            A label selector requirement is a selector that contains values, a key, and an operator that
                            relates the key and values.
                          properties:
                            key:
                              description: key is the label key that the selector
                                applies to.
                              type: string
                            operator:
                              description: |-
                                operator represents a key's relationship to a set of values.
                                Valid operators are In, NotIn, Exists and DoesNotExist.
                              type: string
                            values:
                              description: |-
                                values is an array of string values. If the operator is In or NotIn,
                                the values array must be non-empty. If the operator is Exists or DoesNotExist,
                                the values array must be empty. This array is replaced during a strategic
                                merge patch.
                              items:
                                type: string
                              type: array
                              x-kubernetes-list-type: atomic
                          required:
                          - key
                          - operator
                          type: object
                        type: array
                        x-kubernetes-list-type: atomic
                      matchLabels:
                        additionalProperties:
                          type: string
                        description: |-
                          matchLabels is a map of {key,value} pairs. A single {key,value} in the matchLabels
                          map is equivalent to an element of matchExpressions, whose key field is "key", the
                          operator is "In", and the values array contains only "value". The requirements are ANDed.
                        type: object
                    type: object
                    x-kubernetes-map-type: atomic
                type: object
              defaultSubnet:
                description: The default subnet name for the VPC
                type: string
              enableBfd:
                description: Enable BFD (Bidirectional Forwarding Detection) for the
                  VPC
                type: boolean
              enableExternal:
                description: Enable external network access for the VPC
                type: boolean
              extraExternalSubnets:
                description: Extra external subnets for provider-network VLAN. Immutable
                  after creation.
                items:
                  type: string
                type: array
              namespaces:
                description: List of namespaces that can use this VPC
                items:
                  type: string
                type: array
              policyRoutes:
                description: Policy routes for the VPC.
                items:
                  properties:
                    action:
                      description: Action of the policy route
                      type: string
                    match:
                      description: Match of the policy route
                      type: string
                    nextHopIP:
                      description: NextHopIP is an optional parameter. It needs to
                        be provided only when 'action' is 'reroute'.
                      type: string
                    priority:
                      description: Priority of the policy route (0-32767)
                      type: integer
                  type: object
                type: array
              staticRoutes:
                description: Static routes for the VPC.
                items:
                  properties:
                    bfdId:
                      type: string
                    cidr:
                      type: string
                    ecmpMode:
                      type: string
                    nextHopIP:
                      type: string
                    policy:
                      type: string
                    routeTable:
                      type: string
                  type: object
                type: array
              vpcPeerings:
                description: VPC peering configurations.
                items:
                  properties:
                    localConnectIP:
                      type: string
                    remoteVpc:
                      type: string
                  type: object
                type: array
            type: object
          status:
            properties:
              bfdPort:
                properties:
                  ip:
                    description: BFD port IP address
                    type: string
                  name:
                    description: BFD port name
                    type: string
                  nodes:
                    description: Nodes where BFD port is deployed
                    items:
                      type: string
                    type: array
                type: object
              conditions:
                description: Conditions represents the latest state of the object
                items:
                  description: Condition describes the state of an object at a certain
                    point.
                  properties:
                    lastTransitionTime:
                      description: Last time the condition transitioned from one status
                        to another.
                      format: date-time
                      type: string
                    lastUpdateTime:
                      description: Last time the condition was probed
                      format: date-time
                      type: string
                    message:
                      description: A human readable message indicating details about
                        the transition.
                      type: string
                    observedGeneration:
                      description: |-
                        ObservedGeneration represents the .metadata.generation that the condition was set based upon.
                        For instance, if .metadata.generation is currently 12, but the .status.conditions[x].observedGeneration is 9,
                        the condition is out of date with respect to the current state of the instance.
                      format: int64
                      type: integer
                    reason:
                      description: The reason for the condition's last transition.
                      type: string
                    status:
                      description: Status of the condition, one of True, False, Unknown.
                      type: string
                    type:
                      description: Type of condition.
                      type: string
                  type: object
                type: array
              default:
                description: Whether this is the default VPC.
                type: boolean
              defaultLogicalSwitch:
                type: string
              enableBfd:
                type: boolean
              enableExternal:
                type: boolean
              extraExternalSubnets:
                description: Extra external subnets for provider-network VLAN. Immutable
                  after creation.
                items:
                  type: string
                type: array
              router:
                type: string
              sctpLoadBalancer:
                type: string
              sctpSessionLoadBalancer:
                type: string
              standby:
                type: boolean
              subnets:
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
              vpcPeerings:
                description: VPC peering configurations.
                items:
                  type: string
                type: array
            type: object
        type: object
    served: true
    storage: true
    subresources:
      status: {}
EOF
# END GENERATED KUBE-OVN CRD BUNDLE

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
      - bgp-confs
      - evpn-confs
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
              ephemeral-storage: 1Gi
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
              ephemeral-storage: 1Gi
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
              ephemeral-storage: 1Gi
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
              ephemeral-storage: 1Gi
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
            ephemeral-storage: 1Gi
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
              ephemeral-storage: 1Gi
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
              ephemeral-storage: 1Gi
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
              ephemeral-storage: 1Gi
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
