# Static IP

Kube-OVN support a static ip for a single Pod and a static IP pool for a workload with multiple pods (deployment/daemonset/statefulset), by adding annotations to the pod spec template.

## For a Single Pod

Use following annotations to stabilize the address
- `ovn.kubernetes.io/ip_address`: stabilize ip address
- `ovn.kubernetes.io/mac_address`: stabilize mac address

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

1. The address **SHOULD** be in the cidr of related switch.
2. The address **SHOULD NOT** be conflicted with already allocated addresses.
3. Static mac address is optional.

## For Workload

Use following annotation to allocate addresses for a workload:
-  `ovn.kubernetes.io/ip_pool`: For deployment/daemonset will random choose an available ip for pod. For statefulset the ip allocation will by the list index order.

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

1. The address **SHOULD** be in the cidr of related switch.
2. The address **SHOULD NOT** be conflicted with already allocated addresses.
3. If ip_pool size less than replicas, exceeding pod will not running.
2. Take care of scaling and update strategy to avoid no available address for new pod.