# Webhook

Kube-OVN supports allocation static IP addresss along with dynamical addresss which means we should hold static IP addresses don't allow others using it.

## Pre-request

- Kube-OVN without webhook
- Cert-Manager

## To install

The webhook needs https so we using cert-manager here to generate the certificate. Normally cert-manager doesn't use `hostNetwork`  so it needs CNI to allocate IP addresses.  As a result, we should install ovn, kube-ovn, cert-manager before webhook.

Example:
Assume you have two deployments have ip conflict.

deployment1.yaml

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  namespace: ovn-test
  name: starter-backend1
  labels:
    app: starter-backend1
spec:
  replicas: 2
  selector:
    matchLabels:
      app: starter-backend1
  template:
    metadata:
      labels:
        app: starter-backend1
      annotations:
        ovn.kubernetes.io/ip_pool: 10.16.0.15,10.16.0.16
    spec:
      containers:
      - name: backend
        image: nginx:alpine
```

```bash
# kubectl create -f deployment1.yaml
deployment.apps/starter-backend1 created
```

deployment2.yaml

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  namespace: ovn-test
  name: starter-backend2
  labels:
    app: starter-backend2
spec:
  replicas: 2
  selector:
    matchLabels:
      app: starter-backend2
  template:
    metadata:
      labels:
        app: starter-backend2
      annotations:
        ovn.kubernetes.io/ip_pool: 10.16.0.15,10.16.0.16
    spec:
      containers:
      - name: backend
        image: nginx:alpine
```

```bash
# kubectl create -f deployment2.yaml
Error from server (overlap): error when creating "deployment2.yaml": admission webhook "pod-ip-validaing.kube-ovn.io" denied the request: overlap
```