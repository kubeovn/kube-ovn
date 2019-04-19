# Static IP

Kube-OVN supports allocation a static IP address for a single Pod, or a static IP pool for a Workload with multiple Pods (Deployment/DaemonSet/StatefulSet). To enable this feature, add the following annotations to the Pod spec template.

## For a single Pod

Use the following annotations to specify the address
- `ovn.kubernetes.io/ip_address`: Specifies IP address
- `ovn.kubernetes.io/mac_address`: Specifies MAC address

Example:
```bash
apiVersion: v1
kind: Pod
metadata:
  name: static-ip
  namespace: ls1
  annotations:
    ovn.kubernetes.io/ip_address: 10.16.0.15
    ovn.kubernetes.io/mac_address: 00:00:00:53:6B:B6
spec:
  containers:
  - name: static-ip
    image: nginx:alpine
```

**Note**:

1. The address **SHOULD** be in the CIDR of related switch.
2. The address **SHOULD NOT** conflict with addresses already allocated.
3. The static MAC address is optional.

## For Workloads

Use the following annotation to allocate addresses for a Workload:
-  `ovn.kubernetes.io/ip_pool`: For Deployments/DaemonSets, we will randomly choose an available IP address for a Pod. For StatefulSets, the IP allocation will follow the order specified in the list.

Example:
```bash
apiVersion: apps/v1
kind: Deployment
metadata:
  namespace: ovn-test
  name: starter-backend
  labels:
    app: starter-backend
spec:
  replicas: 2
  selector:
    matchLabels:
      app: starter-backend
  template:
    metadata:
      labels:
        app: starter-backend
      annotations:
        ovn.kubernetes.io/ip_pool: 10.16.0.15,10.16.0.16,10.16.0.17
    spec:
      containers:
      - name: backend
        image: nginx:alpine
```

**Note**:

1. The address **SHOULD** be in the CIDR of the related switch.
2. The address **SHOULD NOT** conflict with addresses already allocated.
3. If the `ip_pool` size is smaller than the replica count, some Pods will not start.
2. Care should be taken for scaling and updates to ensure there are addresses available for new Pods.