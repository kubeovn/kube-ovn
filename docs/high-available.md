# High available for ovn db

ovs support clustered database. If want to use high-available database in kube-ovn,
modify ovn-central deployment in yamls/ovn.yaml.

Change the replicas to 3, and add NODE_IPS environment var.
```yaml
      replicas: 3
      containers:
        - name: ovn-central
          image: "index.alauda.cn/alaudak8s/kube-ovn-db:dev1"
          imagePullPolicy: Always
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: NODE_IPS
              value: 192.168.55.10, 192.168.55.11, 192.168.55.12
```