#!/usr/bin/env bash
set -euo pipefail

REGISTRY="index.alauda.cn/alaudak8s"
NAMESPACE="kube-system"                # The ns to deploy kube-ovn
POD_CIDR="10.16.0.0/16"                # Do NOT overlap with NODE/SVC/JOIN CIDR
SVC_CIDR="10.96.0.0/12"                # Do NOT overlap with NODE/POD/JOIN CIDR
JOIN_CIDR="100.64.0.0/16"              # Do NOT overlap with NODE/POD/SVC CIDR
LABEL="node-role.kubernetes.io/master" # The node label to deploy OVN DB
IFACE=""                               # The nic to support container network, if empty will use the nic that the default route use
VERSION="v1.1.0"

echo "[Step 1] Label kube-ovn-master node"
count=$(kubectl get no -l$LABEL --no-headers -o wide | wc -l | sed 's/ //g')
if [ "$count" = "0" ]; then
  echo "ERROR: No node with label $LABEL"
  exit 1
fi
kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
kubectl label no -l$LABEL kube-ovn/role=master --overwrite
echo "-------------------------------"
echo ""

echo "[Step 2] Install OVN components"
addresses=$(kubectl get no -lkube-ovn/role=master --no-headers -o wide | awk '{print $6}' | tr \\n ',')
echo "Install OVN DB in $addresses"

cat <<EOF > kube-ovn-crd.yaml
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: ips.kubeovn.io
spec:
  group: kubeovn.io
  version: v1
  scope: Cluster
  names:
    plural: ips
    singular: ip
    kind: IP
    shortNames:
      - ip
  additionalPrinterColumns:
    - name: Provider
      type: string
      JSONPath: .spec.provider
    - name: IP
      type: string
      JSONPath: .spec.ipAddress
    - name: Mac
      type: string
      JSONPath: .spec.macAddress
    - name: Node
      type: string
      JSONPath: .spec.nodeName
    - name: Subnet
      type: string
      JSONPath: .spec.subnet
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: subnets.kubeovn.io
spec:
  group: kubeovn.io
  version: v1
  scope: Cluster
  names:
    plural: subnets
    singular: subnet
    kind: Subnet
    shortNames:
      - subnet
  subresources:
    status: {}
  additionalPrinterColumns:
    - name: Protocol
      type: string
      JSONPath: .spec.protocol
    - name: CIDR
      type: string
      JSONPath: .spec.cidrBlock
    - name: Private
      type: boolean
      JSONPath: .spec.private
    - name: NAT
      type: boolean
      JSONPath: .spec.natOutgoing
    - name: Default
      type: boolean
      JSONPath: .spec.default
    - name: GatewayType
      type: string
      JSONPath: .spec.gatewayType
    - name: Used
      type: integer
      JSONPath: .status.usingIPs
    - name: Available
      type: integer
      JSONPath: .status.availableIPs
  validation:
    openAPIV3Schema:
      properties:
        spec:
          required: ["cidrBlock"]
          properties:
            cidrBlock:
              type: "string"
            gateway:
              type: "string"
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: vlans.kubeovn.io
spec:
  group: kubeovn.io
  version: v1
  scope: Cluster
  names:
    plural: vlans
    singular: vlan
    kind: Vlan
    shortNames:
      - vlan
  additionalPrinterColumns:
    - name: VlanID
      type: string
      JSONPath: .spec.vlanId
    - name: ProviderInterfaceName
      type: string
      JSONPath: .spec.providerInterfaceName
    - name: Subnet
      type: string
      JSONPath: .spec.subnet
---
apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: networks.kubeovn.io
spec:
  group: kubeovn.io
  version: v1
  scope: Cluster
  names:
    plural: networks
    singular: network
    kind: Network
    shortNames:
      - network
  additionalPrinterColumns:
    - name: NetworkType
      type: string
      JSONPath: .spec.networkType
    - name: DefaultSubnet
      type: string
      JSONPath: .spec.defaultSubnet
    - name: NodeSubnet
      type: string
      JSONPath: .spec.nodeSubnet
    - name: MasterNode
      type: string
      JSONPath: .spec.masterNode
    - name: PprofPort
      type: integer
      JSONPath: .spec.pprofPort
    - name: ProviderName
      type: string
      JSONPath: .spec.providerName
EOF

cat <<EOF > ovn.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: ovn-config
  namespace: ${NAMESPACE}

---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ovn
  namespace:  ${NAMESPACE}

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
      - subnets
      - subnets/status
      - ips
      - vlans
      - networks
    verbs:
      - "*"
  - apiGroups:
      - ""
    resources:
      - pods
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
    resources:
      - networkpolicies
      - services
      - endpoints
      - statefulsets
      - daemonsets
    verbs:
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
    namespace:  ${NAMESPACE}

---
kind: Service
apiVersion: v1
metadata:
  name: ovn-nb
  namespace:  ${NAMESPACE}
spec:
  ports:
    - name: ovn-nb
      protocol: TCP
      port: 6641
      targetPort: 6641
  type: ClusterIP
  selector:
    app: ovn-central
    ovn-nb-leader: "true"
  sessionAffinity: None

---
kind: Service
apiVersion: v1
metadata:
  name: ovn-sb
  namespace:  ${NAMESPACE}
spec:
  ports:
    - name: ovn-sb
      protocol: TCP
      port: 6642
      targetPort: 6642
  type: ClusterIP
  selector:
    app: ovn-central
    ovn-sb-leader: "true"
  sessionAffinity: None

---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: ovn-central
  namespace:  ${NAMESPACE}
  annotations:
    kubernetes.io/description: |
      OVN components: northd, nb and sb.
spec:
  replicas: $count
  strategy:
    rollingUpdate:
      maxSurge: 0%
      maxUnavailable: 100%
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
      - operator: Exists
        effect: NoSchedule
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
          imagePullPolicy: IfNotPresent
          command: ["/kube-ovn/start-db.sh"]
          securityContext:
            capabilities:
              add: ["SYS_NICE"]
          env:
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
          resources:
            requests:
              cpu: 500m
              memory: 300Mi
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
          readinessProbe:
            exec:
              command:
                - sh
                - /kube-ovn/ovn-is-leader.sh
            periodSeconds: 3
          livenessProbe:
            exec:
              command:
                - sh
                - /kube-ovn/ovn-healthcheck.sh
            initialDelaySeconds: 30
            periodSeconds: 7
            failureThreshold: 5
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

---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: ovs-ovn
  namespace:  ${NAMESPACE}
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
      - operator: Exists
        effect: NoSchedule
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
      hostNetwork: true
      hostPID: true
      containers:
        - name: openvswitch
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: IfNotPresent
          command: ["/kube-ovn/start-ovs.sh"]
          securityContext:
            runAsUser: 0
            privileged: true
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
          volumeMounts:
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
            - mountPath: /var/log/openvswitch
              name: host-log-ovs
            - mountPath: /var/log/ovn
              name: host-log-ovn
          readinessProbe:
            exec:
              command:
                - sh
                - /kube-ovn/ovs-healthcheck.sh
            periodSeconds: 5
          livenessProbe:
            exec:
              command:
                - sh
                - /kube-ovn/ovs-healthcheck.sh
            initialDelaySeconds: 10
            periodSeconds: 5
            failureThreshold: 5
          resources:
            requests:
              cpu: 200m
              memory: 300Mi
            limits:
              cpu: 1000m
              memory: 800Mi
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
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
EOF

kubectl apply -f kube-ovn-crd.yaml
kubectl apply -f ovn.yaml
kubectl rollout status deployment/ovn-central -n ${NAMESPACE}
echo "-------------------------------"
echo ""

echo "[Step 3] Install Kube-OVN"

cat <<EOF > kube-ovn.yaml
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: kube-ovn-controller
  namespace:  ${NAMESPACE}
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
      - operator: Exists
        effect: NoSchedule
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
          imagePullPolicy: IfNotPresent
          command:
          - /kube-ovn/start-controller.sh
          args:
          - --default-cidr=$POD_CIDR
          - --node-switch-cidr=$JOIN_CIDR
          - --network-type=vlan
          env:
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
          readinessProbe:
            exec:
              command:
                - sh
                - /kube-ovn/kube-ovn-controller-healthcheck.sh
            periodSeconds: 3
          livenessProbe:
            exec:
              command:
                - sh
                - /kube-ovn/kube-ovn-controller-healthcheck.sh
            initialDelaySeconds: 300
            periodSeconds: 7
            failureThreshold: 5
      nodeSelector:
        kubernetes.io/os: "linux"

---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: kube-ovn-cni
  namespace:  ${NAMESPACE}
  annotations:
    kubernetes.io/description: |
      This daemon set launches the kube-ovn cni daemon.
spec:
  selector:
    matchLabels:
      app: kube-ovn-cni
  updateStrategy:
    type: OnDelete
  template:
    metadata:
      labels:
        app: kube-ovn-cni
        component: network
        type: infra
    spec:
      tolerations:
      - operator: Exists
        effect: NoSchedule
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
      hostNetwork: true
      hostPID: true
      initContainers:
      - name: install-cni
        image: "$REGISTRY/kube-ovn:$VERSION"
        imagePullPolicy: IfNotPresent
        command: ["/kube-ovn/install-cni.sh"]
        securityContext:
          runAsUser: 0
          privileged: true
        volumeMounts:
          - mountPath: /etc/cni/net.d
            name: cni-conf
          - mountPath: /opt/cni/bin
            name: cni-bin
      containers:
      - name: cni-server
        image: "$REGISTRY/kube-ovn:$VERSION"
        imagePullPolicy: IfNotPresent
        command:
          - sh
          - /kube-ovn/start-cniserver.sh
        args:
          - --enable-mirror=true
          - --encap-checksum=true
          - --service-cluster-ip-range=$SVC_CIDR
          - --iface=${IFACE}
        securityContext:
          capabilities:
            add: ["NET_ADMIN", "SYS_ADMIN", "SYS_PTRACE"]
        env:
          - name: POD_IP
            valueFrom:
              fieldRef:
                fieldPath: status.podIP
          - name: KUBE_NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
        volumeMounts:
          - mountPath: /run/openvswitch
            name: host-run-ovs
          - mountPath: /run/ovn
            name: host-run-ovn
          - mountPath: /var/run/netns
            name: host-ns
            mountPropagation: HostToContainer
        readinessProbe:
          exec:
            command:
              - nc
              - -z
              - -w3
              - 127.0.0.1
              - "10665"
          periodSeconds: 3
        livenessProbe:
          exec:
            command:
              - nc
              - -z
              - -w3
              - 127.0.0.1
              - "10665"
          initialDelaySeconds: 30
          periodSeconds: 7
          failureThreshold: 5
      nodeSelector:
        kubernetes.io/os: "linux"
      volumes:
        - name: host-run-ovs
          hostPath:
            path: /run/openvswitch
        - name: host-run-ovn
          hostPath:
            path: /run/ovn
        - name: cni-conf
          hostPath:
            path: /etc/cni/net.d
        - name: cni-bin
          hostPath:
            path: /opt/cni/bin
        - name: host-ns
          hostPath:
            path: /var/run/netns

---
kind: DaemonSet
apiVersion: apps/v1
metadata:
  name: kube-ovn-pinger
  namespace:  ${NAMESPACE}
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
      tolerations:
        - operator: Exists
          effect: NoSchedule
      serviceAccountName: ovn
      hostPID: true
      containers:
        - name: pinger
          image: "$REGISTRY/kube-ovn:$VERSION"
          command: ["/kube-ovn/kube-ovn-pinger", "--external-address=114.114.114.114"]
          imagePullPolicy: IfNotPresent
          securityContext:
            runAsUser: 0
            privileged: false
          env:
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
          resources:
            requests:
              cpu: 100m
              memory: 300Mi
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
        - name: host-log-ovn
          hostPath:
            path: /var/log/ovn
---
kind: Service
apiVersion: v1
metadata:
  name: kube-ovn-pinger
  namespace:  ${NAMESPACE}
  labels:
    app: kube-ovn-pinger
spec:
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
  namespace:  ${NAMESPACE}
  labels:
    app: kube-ovn-controller
spec:
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
  namespace:  ${NAMESPACE}
  labels:
    app: kube-ovn-cni
spec:
  selector:
    app: kube-ovn-cni
  ports:
    - port: 10665
      name: metrics
EOF

kubectl apply -f kube-ovn.yaml
kubectl rollout status deployment/kube-ovn-controller -n ${NAMESPACE}
echo "-------------------------------"
echo ""

echo "[Step 4] Delete pod that not in host network mode"
for ns in $(kubectl get ns --no-headers -o  custom-columns=NAME:.metadata.name); do
  for pod in $(kubectl get pod --no-headers -n "$ns" --field-selector spec.restartPolicy=Always -o custom-columns=NAME:.metadata.name,HOST:spec.hostNetwork | awk '{if ($2!="true") print $1}'); do
    kubectl delete pod "$pod" -n "$ns"
  done
done

kubectl rollout status daemonset/kube-ovn-pinger -n ${NAMESPACE}
kubectl rollout status deployment/coredns -n kube-system
echo "-------------------------------"
echo ""

echo "[Step 5] Install kubectl plugin"
mkdir -p /usr/local/bin
cat <<\EOF > /usr/local/bin/kubectl-ko
#!/bin/bash
set -euo pipefail

KUBE_OVN_NS=kube-system
OVN_NB_POD=
OVN_SB_POD=

showHelp(){
  echo "kubectl ko {subcommand} [option...]"
  echo "Available Subcommands:"
  echo "  nbctl [ovn-nbctl options ...]    invoke ovn-nbctl"
  echo "  sbctl [ovn-sbctl options ...]    invoke ovn-sbctl"
  echo "  vsctl {nodeName} [ovs-vsctl options ...]   invoke ovs-vsctl on selected node"
  echo "  tcpdump {namespace/podname} [tcpdump options ...]     capture pod traffic"
  echo "  trace {namespace/podname} {target ip address} {icmp|tcp|udp} [target tcp or udp port]    trace ovn microflow of specific packet"
  echo "  diagnose {all|node} [nodename]    diagnose connectivity of all nodes or a specific node"
}

tcpdump(){
  namespacedPod="$1"; shift
  namespace=$(echo "$namespacedPod" | cut -d "/" -f1)
  podName=$(echo "$namespacedPod" | cut -d "/" -f2)
  if [ "$podName" = "$namespacedPod" ]; then
    nodeName=$(kubectl get pod "$podName" -o jsonpath={.spec.nodeName})
    mac=$(kubectl get pod "$podName" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/mac_address})
    hostNetwork=$(kubectl get pod "$podName" -o jsonpath={.spec.hostNetwork})
  else
    nodeName=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.nodeName})
    mac=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/mac_address})
    hostNetwork=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.hostNetwork})
  fi

  if [ -z "$nodeName" ]; then
    echo "Pod $namespacedPod not exists on any node"
    exit 1
  fi

  if [ -z "$mac" ] && [ "$hostNetwork" != "true" ]; then
     echo "pod mac address not ready"
     exit 1
  fi

  ovnCni=$(kubectl get pod -n $KUBE_OVN_NS -o wide| grep kube-ovn-cni| grep " $nodeName " | awk '{print $1}')
  if [ -z "$ovnCni" ]; then
    echo "kube-ovn-cni not exist on node $nodeName"
    exit 1
  fi

  if [ "$hostNetwork" = "true" ]; then
    set -x
    kubectl exec -it "$ovnCni" -n $KUBE_OVN_NS -- tcpdump -nn "$@"
  else
    nicName=$(kubectl exec -it "$ovnCni" -n $KUBE_OVN_NS -- ovs-vsctl --data=bare --no-heading --columns=name find interface mac_in_use="${mac//:/\\:}" | tr -d '\r')
    if [ -z "$nicName" ]; then
      echo "nic doesn't exist on node $nodeName"
      exit 1
    fi
    set -x
    kubectl exec -it "$ovnCni" -n $KUBE_OVN_NS -- tcpdump -nn -i "$nicName" "$@"
  fi
}

trace(){
  namespacedPod="$1"
  namespace=$(echo "$1" | cut -d "/" -f1)
  podName=$(echo "$1" | cut -d "/" -f2)
  if [ "$podName" = "$1" ]; then
    echo "namespace is required"
    exit 1
  fi

  podIP=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/ip_address})
  mac=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/mac_address})
  ls=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.metadata.annotations.ovn\\.kubernetes\\.io/logical_switch})
  hostNetwork=$(kubectl get pod "$podName" -n "$namespace" -o jsonpath={.spec.hostNetwork})

  if [ "$hostNetwork" = "true" ]; then
    echo "Can not trace host network pod"
    exit 1
  fi

  if [ -z "$ls" ]; then
    echo "pod address not ready"
    exit 1
  fi

  gwMac=$(kubectl exec -it $OVN_NB_POD -n $KUBE_OVN_NS -- ovn-nbctl --data=bare --no-heading --columns=mac find logical_router_port name=ovn-cluster-"$ls" | tr -d '\r')

  if [ -z "$gwMac" ]; then
    echo "get gw mac failed"
    exit 1
  fi

  dst="$2"
  if [ -z "$dst" ]; then
    echo "need a target ip address"
    exit 1
  fi

  type="$3"

  case $type in
    icmp)
      set -x
      kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -- ovn-trace --ct=new "$ls" "inport == \"$podName.$namespace\" && ip.ttl == 64 && icmp && eth.src == $mac && ip4.src == $podIP && eth.dst == $gwMac && ip4.dst == $dst"
      ;;
    tcp|udp)
      set -x
      kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -- ovn-trace --ct=new "$ls" "inport == \"$podName.$namespace\" && ip.ttl == 64 && eth.src == $mac && ip4.src == $podIP && eth.dst == $gwMac && ip4.dst == $dst && $type.src == 10000 && $type.dst == $4"
      ;;
    *)
      echo "type $type not supported"
      echo "kubectl ko trace {namespace/podname} {target ip address} {icmp|tcp|udp} [target tcp or udp port]"
      ;;
  esac
}

vsctl(){
  nodeName="$1"; shift
  kubectl get no "$nodeName" > /dev/null
  ovsPod=$(kubectl get pod -n $KUBE_OVN_NS -o wide | grep " $nodeName " | grep ovs-ovn | awk '{print $1}')
  if [ -z "$ovsPod" ]; then
      echo "ovs pod  doesn't exist on node $nodeName"
      exit 1
    fi
  kubectl exec "$ovsPod" -n $KUBE_OVN_NS -- ovs-vsctl "$@"
}

diagnose(){
  kubectl get crd subnets.kubeovn.io
  kubectl get crd ips.kubeovn.io

  checkDaemonSet kube-proxy
  checkDeployment ovn-central
  checkDeployment kube-ovn-controller
  checkDaemonSet kube-ovn-cni
  checkDaemonSet ovs-ovn
  checkDeployment coredns

  type="$1"
  case $type in
    all)
      echo "### kube-ovn-controller recent log"
      kubectl logs -n $KUBE_OVN_NS -l app=kube-ovn-controller --tail=15
      echo ""
      pingers=$(kubectl get pod -n $KUBE_OVN_NS | grep kube-ovn-pinger | awk '{print $1}')
      for pinger in $pingers
      do
        nodeName=$(kubectl get pod "$pinger" -n "$KUBE_OVN_NS" -o jsonpath={.spec.nodeName})
        echo "### start to diagnose node $nodeName"
        echo "#### ovn-controller log:"
        kubectl exec -n $KUBE_OVN_NS -it "$pinger" -- tail /var/log/ovn/ovn-controller.log
        echo ""
        kubectl exec -n $KUBE_OVN_NS -it "$pinger" -- /kube-ovn/kube-ovn-pinger --mode=job
        echo "### finish diagnose node $nodeName"
        echo ""
      done
      ;;
    node)
      nodeName="$2"
      kubectl get no "$nodeName" > /dev/null
      pinger=$(kubectl get pod -n $KUBE_OVN_NS -o wide | grep kube-ovn-pinger | grep " $nodeName " | awk '{print $1}')
      echo "### start to diagnose node nodeName"
      echo "#### ovn-controller log:"
      kubectl exec -n $KUBE_OVN_NS -it "$pinger" -- tail /var/log/ovn/ovn-controller.log
      echo ""
      kubectl exec -n $KUBE_OVN_NS -it "$pinger" -- /kube-ovn/kube-ovn-pinger --mode=job
      echo "### finish diagnose node nodeName"
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

if [ $# -lt 1 ]; then
  showHelp
  exit 0
else
  subcommand="$1"; shift
fi

getOvnCentralPod

case $subcommand in
  nbctl)
    kubectl exec "$OVN_NB_POD" -n $KUBE_OVN_NS -- ovn-nbctl "$@"
    ;;
  sbctl)
    kubectl exec "$OVN_SB_POD" -n $KUBE_OVN_NS -- ovn-sbctl "$@"
    ;;
  vsctl)
    vsctl "$@"
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
  *)
    showHelp
    ;;
esac
EOF

chmod +x /usr/local/bin/kubectl-ko
echo "-------------------------------"
echo ""

echo "[Step 6] Run network diagnose"
kubectl ko diagnose all

echo "-------------------------------"
echo ""
