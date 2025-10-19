# Quick Start: Kube-OVN Non-Primary CNI

This guide provides a quick example of running Kube-OVN as a secondary CNI alongside Cilium.

## Prerequisites

- Kubernetes cluster with Cilium as primary CNI
- Multus CNI installed
- Helm 3.x

## Step 1: Install Kube-OVN in Non-Primary Mode

```bash
helm install kube-ovn ./charts/kube-ovn-v2 \
  --namespace kube-system \
  --set cni.nonPrimaryCNI=true
```

## Step 2: Create a VPC and Subnet

```yaml
# vpc-secondary.yaml
apiVersion: kubeovn.io/v1
kind: Vpc
metadata:
  name: vpc-secondary
spec:
  namespaces:
  - default
---
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: subnet-secondary
spec:
  vpc: vpc-secondary
  cidr: "10.100.0.0/16"
  gateway: "10.100.0.1"
  provider: kube-ovn-secondary.default.ovn
```

Apply the configuration:
```bash
kubectl apply -f vpc-secondary.yaml
```

## Step 3: Create Network Attachment Definition

```yaml
# nad-secondary.yaml
apiVersion: k8s.cni.cncf.io/v1
kind: NetworkAttachmentDefinition
metadata:
  name: kube-ovn-secondary
  namespace: default
spec:
  config: |
    {
      "cniVersion": "0.3.1",
      "type": "kube-ovn",
      "server_socket": "/run/openvswitch/kube-ovn-daemon.sock",
      "provider": "kube-ovn-secondary.default.ovn"
    }
```

Apply the NAD:
```bash
kubectl apply -f nad-secondary.yaml
```

## Step 4: Deploy a Multi-Network Pod

```yaml
# multi-network-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: multi-network-pod
  annotations:
    k8s.v1.cni.cncf.io/networks: default/kube-ovn-secondary
spec:
  containers:
  - name: app
    image: nginx
    ports:
    - containerPort: 80
```

Deploy the pod:
```bash
kubectl apply -f multi-network-pod.yaml
```

## Step 5: Verify the Setup

Check the pod has multiple interfaces:
```bash
kubectl exec multi-network-pod -- ip addr show
```

Expected output:
```
1: lo: <LOOPBACK,UP,LOWER_UP> mtu 65536 qdisc noqueue state UNKNOWN qlen 1000
    link/loopback 00:00:00:00:00:00 brd 00:00:00:00:00:00
    inet 127.0.0.1/8 scope host lo
    
2: eth0@if123: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP
    link/ether 02:42:ac:11:00:02 brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 10.244.0.5/24 brd 10.244.0.255 scope global eth0  # Cilium interface
    
3: net1@if124: <BROADCAST,MULTICAST,UP,LOWER_UP> mtu 1500 qdisc noqueue state UP
    link/ether 00:00:00:ab:cd:ef brd ff:ff:ff:ff:ff:ff link-netnsid 0
    inet 10.100.0.2/16 brd 10.100.255.255 scope global net1  # Kube-OVN interface
```

Check network status annotation:
```bash
kubectl get pod multi-network-pod -o jsonpath='{.metadata.annotations.k8s\.v1\.cni\.cncf\.io/networks-status}' | jq .
```

## Cleanup

```bash
kubectl delete pod multi-network-pod
kubectl delete nad kube-ovn-secondary
kubectl delete -f vpc-secondary.yaml
helm uninstall kube-ovn -n kube-system
```

## Next Steps

- See [NON-PRIMARY-CNI.md](NON-PRIMARY-CNI.md) for detailed documentation
- Explore VPC NAT Gateway configuration for external connectivity
- Configure security groups and QoS policies for secondary networks
