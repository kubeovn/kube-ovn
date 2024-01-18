#!/usr/bin/env bash
set -euo pipefail

REGISTRY="kubeovn"
VERSION="v1.13.0"
TS_NUM=${TS_NUM:-3}
IMAGE_PULL_POLICY="IfNotPresent"
addresses=$(kubectl get no -lkube-ovn/role=master --no-headers -o wide | awk '{print $6}' | tr \\n ',')
count=$(kubectl get no -lkube-ovn/role=master --no-headers | wc -l)
OVN_LEADER_PROBE_INTERVAL=${OVN_LEADER_PROBE_INTERVAL:-5}

cat <<EOF > ovn-ic-server.yaml
---
kind: Deployment
apiVersion: apps/v1
metadata:
  name: ovn-ic-server
  namespace: kube-system
  annotations:
    kubernetes.io/description: |
      OVN IC Server
spec:
  replicas: $count
  strategy:
    rollingUpdate:
      maxSurge: 0
      maxUnavailable: 1
    type: RollingUpdate
  selector:
    matchLabels:
      app: ovn-ic-server
  template:
    metadata:
      labels:
        app: ovn-ic-server
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
                  app: ovn-ic-server
              topologyKey: kubernetes.io/hostname
      priorityClassName: system-cluster-critical
      serviceAccountName: ovn
      hostNetwork: true
      containers:
        - name: ovn-ic-server
          image: "$REGISTRY/kube-ovn:$VERSION"
          imagePullPolicy: $IMAGE_PULL_POLICY
          command: ["/kube-ovn/start-ic-db.sh"]
          securityContext:
            capabilities:
              add: ["SYS_NICE"]
          env:
            - name: ENABLE_SSL
              value: "false"
            - name: TS_NUM
              value: "$TS_NUM"
            - name: NODE_IPS
              value: $addresses
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: OVN_LEADER_PROBE_INTERVAL
              value: "$OVN_LEADER_PROBE_INTERVAL"
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
          readinessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovn-ic-healthcheck.sh
            periodSeconds: 15
            timeoutSeconds: 45
          livenessProbe:
            exec:
              command:
                - bash
                - /kube-ovn/ovn-ic-healthcheck.sh
            initialDelaySeconds: 30
            periodSeconds: 15
            failureThreshold: 5
            timeoutSeconds: 4
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
        - name: kube-ovn-tls
          secret:
            optional: true
            secretName: kube-ovn-tls
EOF

kubectl apply -f ovn-ic-server.yaml
kubectl rollout status deployment/ovn-ic-server -n kube-system --timeout 600s

echo "OVN IC Server installed Successfully"
