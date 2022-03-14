#!/bin/bash
set -eo pipefail

IMAGE=kubeovn/kube-ovn:v1.9.1

echo "[Step 0/8] Update CRD"
cat <<EOF > kube-ovn-crd-1.9.yaml
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
                dnatRules:
                  type: array
                  items:
                    type: object
                    properties:
                      eip:
                        type: string
                      externalPort:
                        type: string
                      internalIp:
                        type: string
                      internalPort:
                        type: string
                      protocol:
                        type: string
                eips:
                  type: array
                  items:
                    type: object
                    properties:
                      eipCIDR:
                        type: string
                      gateway:
                        type: string
                floatingIpRules:
                  type: array
                  items:
                    type: object
                    properties:
                      eip:
                        type: string
                      internalIp:
                        type: string
                lanIp:
                  type: string
                snatRules:
                  type: array
                  items:
                    type: object
                    properties:
                      eip:
                        type: string
                      internalCIDR:
                        type: string
                subnet:
                  type: string
                vpc:
                  type: string
                selector:
                  type: array
                  items:
                    type: string
      subresources:
        status: {}
  conversion:
    strategy: None
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

kubectl apply -f kube-ovn-crd-1.9.yaml
echo "-------------------------------"
echo ""

echo "[Step 1/8] Update Cluster Role"

cat <<EOF > kube-ovn-cluster-role-1.9.yaml
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
      - vlans
      - provider-networks
      - provider-networks/status
      - security-groups
      - security-groups/status
      - htbqoses
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
EOF

kubectl apply -f kube-ovn-cluster-role-1.9.yaml
echo "-------------------------------"
echo ""

echo "[Step 2/8] Update ovn-central"
kubectl set image deployment/ovn-central -n kube-system ovn-central="$IMAGE"
kubectl rollout status deployment/ovn-central -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 3/8] Update ovs-ovn"
kubectl set image ds/ovs-ovn -n kube-system openvswitch="$IMAGE"
kubectl delete pod -n kube-system -lapp=ovs
echo "-------------------------------"
echo ""

echo "[Step 4/8] Update kube-ovn-controller"
kubectl set image deployment/kube-ovn-controller -n kube-system kube-ovn-controller="$IMAGE"
if [[ ! $(kubectl get deploy -n kube-system kube-ovn-controller -o jsonpath='{.spec.template.spec.containers[0].args}') =~ "logtostderr" ]]; then
  kubectl patch deploy/kube-ovn-controller -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--logtostderr=false"}, {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--alsologtostderr=true"}, {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--log_file=/var/log/kube-ovn/kube-ovn-controller.log"}, {"op": "add", "path": "/spec/template/spec/containers/0/volumeMounts/-", "value": {"mountPath": "/var/log/kube-ovn", "name": "kube-ovn-log"}}, {"op": "add", "path": "/spec/template/spec/volumes/-", "value": {"hostPath": {"path": "/var/log/kube-ovn"}, "name": "kube-ovn-log"}}]'
fi
kubectl patch deploy/kube-ovn-controller -n kube-system --type='json' -p='[{"op": "replace", "path": "/spec/template/spec/containers/0/readinessProbe/exec/command", "value": ["/kube-ovn/kube-ovn-controller-healthcheck"]}, {"op": "replace", "path": "/spec/template/spec/containers/0/livenessProbe/exec/command", "value": ["/kube-ovn/kube-ovn-controller-healthcheck"]}]'
kubectl rollout status deployment/kube-ovn-controller -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 5/8] Update kube-ovn-cni"
kubectl set image ds/kube-ovn-cni -n kube-system cni-server="$IMAGE"
if [[ ! $(kubectl get ds -n kube-system kube-ovn-cni -o jsonpath='{.spec.template.spec.containers[0].args}') =~ "logtostderr" ]]; then
  kubectl patch ds/kube-ovn-cni -n kube-system --type='json' -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--logtostderr=false"}, {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--alsologtostderr=true"}, {"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--log_file=/var/log/kube-ovn/kube-ovn-cni.log"}, {"op": "add", "path": "/spec/template/spec/containers/0/volumeMounts/-", "value": {"mountPath": "/var/log/kube-ovn", "name": "kube-ovn-log"}}, {"op": "add", "path": "/spec/template/spec/volumes/-", "value": {"hostPath": {"path": "/var/log/kube-ovn"}, "name": "kube-ovn-log"}}]'
fi
kubectl rollout status daemonset/kube-ovn-cni -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 6/8] Update kube-ovn-pinger"
kubectl set image ds/kube-ovn-pinger -n kube-system pinger="$IMAGE"
kubectl rollout status daemonset/kube-ovn-pinger -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 7/8] Update kube-ovn-monitor"
kubectl set image deployment/kube-ovn-monitor -n kube-system kube-ovn-monitor="$IMAGE"
kubectl rollout status deployment/kube-ovn-monitor -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 8/8] Update kubectl ko plugin"
mkdir -p /usr/local/bin
cat <<\EOF > /usr/local/bin/kubectl-ko
#!/bin/bash
set -euo pipefail

KUBE_OVN_NS=kube-system
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
  echo "  trace {namespace/podname} {target ip address} {icmp|tcp|udp} [target tcp or udp port]    trace ovn microflow of specific packet"
  echo "  diagnose {all|node} [nodename]    diagnose connectivity of all nodes or a specific node"
  echo "  reload restart all kube-ovn components"
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

  ovnCni=$(kubectl get pod -n $KUBE_OVN_NS -o wide| grep kube-ovn-cni| grep " $nodeName " | awk '{print $1}')
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
  namespace=$(echo "$1" | cut -d "/" -f1)
  podName=$(echo "$1" | cut -d "/" -f2)
  if [ "$podName" = "$1" ]; then
    namespace="default"
  fi

  dst="$2"
  if [ -z "$dst" ]; then
    echo "need a target ip address"
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

  hostNetwork=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.hostNetwork})
  if [ "$hostNetwork" = "true" ]; then
    echo "Can not trace host network pod"
    exit 1
  fi

  ls=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/logical_switch})
  if [ -z "$ls" ]; then
    echo "pod address not ready"
    exit 1
  fi

  podIPs=($(kubectl get pod "$podName" -n "$namespace" -o jsonpath="{.status.podIPs[*].ip}"))
  if [ ${#podIPs[@]} -eq 0 ]; then
    podIPs=($(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/ip_address} | sed 's/,/ /g'))
    if [ ${#podIPs[@]} -eq 0 ]; then
      echo "pod address not ready"
      exit 1
    fi
  fi

  mac=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/mac_address})
  nodeName=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.nodeName})

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
    echo "Pod has no IPv$af address"
    exit 1
  fi

  gwMac=""
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

    ovnCni=$(kubectl get pod -n $KUBE_OVN_NS -o wide | grep -w kube-ovn-cni | grep " $nodeName " | awk '{print $1}')
    if [ -z "$ovnCni" ]; then
      echo "No kube-ovn-cni Pod running on node $nodeName"
      exit 1
    fi

    nicName=$(kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- ovs-vsctl --data=bare --no-heading --columns=name find interface external-ids:iface-id="$podName"."$namespace" | tr -d '\r')
    if [ -z "$nicName" ]; then
      echo "nic doesn't exist on node $nodeName"
      exit 1
    fi

    podNicType=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/pod_nic_type})
    podNetNs=$(kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- ovs-vsctl --data=bare --no-heading get interface "$nicName" external-ids:pod_netns | tr -d '\r' | sed -e 's/^"//' -e 's/"$//')
    if [ "$podNicType" != "internal-port" ]; then
      nicName="eth0"
    fi

    if [[ "$gateway" =~ .*:.* ]]; then
      cmd="ndisc6 -q $gateway $nicName"
      output=$(kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" ndisc6 -q "$gateway" "$nicName")
    else
      cmd="arping -c3 -C1 -i1 -I $nicName $gateway"
      output=$(kubectl exec "$ovnCni" -n $KUBE_OVN_NS -- nsenter --net="$podNetNs" arping -c3 -C1 -i1 -I "$nicName" "$gateway")
    fi

    if [ $? -ne 0 ]; then
      echo "failed to run '$cmd' in Pod's netns"
      exit 1
    fi
    gwMac=$(echo "$output" | grep -o -E '([[:xdigit:]]{1,2}:){5}[[:xdigit:]]{1,2}')
  else
    lr=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/logical_router})
    if [ -z "$lr" ]; then
      lr=$(kubectl get subnet "$ls" -o jsonpath={.spec.vpc})
    fi
    gwMac=$(kubectl exec $OVN_NB_POD -n $KUBE_OVN_NS -c ovn-central -- ovn-nbctl --data=bare --no-heading --columns=mac find logical_router_port name="$lr"-"$ls" | tr -d '\r')
  fi

  if [ -z "$gwMac" ]; then
    echo "get gw mac failed"
    exit 1
  fi

  type="$3"
  case $type in
    icmp)
      set -x
      kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-trace --ct=new "$ls" "inport == \"$podName.$namespace\" && ip.ttl == 64 && icmp && eth.src == $mac && ip$af.src == $podIP && eth.dst == $gwMac && ip$af.dst == $dst"
      ;;
    tcp|udp)
      set -x
      kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -c ovn-central -- ovn-trace --ct=new "$ls" "inport == \"$podName.$namespace\" && ip.ttl == 64 && eth.src == $mac && ip$af.src == $podIP && eth.dst == $gwMac && ip$af.dst == $dst && $type.src == 10000 && $type.dst == $4"
      ;;
    *)
      echo "type $type not supported"
      echo "kubectl ko trace {namespace/podname} {target ip address} {icmp|tcp|udp} [target tcp or udp port]"
      exit 1
      ;;
  esac

  set +x
  echo "--------"
  echo "Start OVS Tracing"
  echo ""
  echo ""

  ovsPod=$(kubectl get pod -n $KUBE_OVN_NS -o wide | grep " $nodeName " | grep ovs-ovn | awk '{print $1}')
  if [ -z "$ovsPod" ]; then
    echo "ovs pod doesn't exist on node $nodeName"
    exit 1
  fi

  inPort=$(kubectl exec "$ovsPod" -n $KUBE_OVN_NS -- ovs-vsctl --format=csv --data=bare --no-heading --columns=ofport find interface external_id:iface-id="$podName"."$namespace")
  case $type in
    icmp)
      set -x
      kubectl exec "$ovsPod" -n $KUBE_OVN_NS -- ovs-appctl ofproto/trace br-int "in_port=$inPort,icmp$proto,${nw}_src=$podIP,${nw}_dst=$dst,dl_src=$mac,dl_dst=$gwMac"
      ;;
    tcp|udp)
      set -x
      kubectl exec "$ovsPod" -n $KUBE_OVN_NS -- ovs-appctl ofproto/trace br-int "in_port=$inPort,$type$proto,${nw}_src=$podIP,${nw}_dst=$dst,dl_src=$mac,dl_dst=$gwMac,${type}_src=1000,${type}_dst=$4"
      ;;
    *)
      echo "type $type not supported"
      echo "kubectl ko trace {namespace/podname} {target ip address} {icmp|tcp|udp} [target tcp or udp port]"
      exit 1
      ;;
  esac
}

xxctl(){
  subcommand="$1"; shift
  nodeName="$1"; shift
  kubectl get no "$nodeName" > /dev/null
  ovsPod=$(kubectl get pod -n $KUBE_OVN_NS -o wide | grep " $nodeName " | grep ovs-ovn | awk '{print $1}')
  if [ -z "$ovsPod" ]; then
    echo "ovs pod  doesn't exist on node $nodeName"
    exit 1
  fi
  kubectl exec "$ovsPod" -n $KUBE_OVN_NS -- ovs-$subcommand "$@"
}

checkLeader(){
  component="$1"; shift
  count=$(kubectl get ep ovn-$component -n $KUBE_OVN_NS -o yaml | grep ip | wc -l)
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

  checkKubeProxy
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
  dsMode=`kubectl get ds -n kube-system | grep kube-proxy || true`
  if [ -z "$dsMode" ]; then
    nodeIps=`kubectl get node -o wide | grep -v "INTERNAL-IP" | awk '{print $6}'`
    for node in $nodeIps
    do
      healthResult=`curl -g -6 -sL -w %{http_code} http://[$node]:10256/healthz -o /dev/null | grep -v 200 || true`
      if [ -n "$healthResult" ]; then
        echo "$node kube-proxy's health check failed"
        exit 1
      fi
    done
  else
    checkDaemonSet kube-proxy
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
  *)
    showHelp
    ;;
esac

EOF

echo "Update Success!"
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
