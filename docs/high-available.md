# High available for ovn db

OVN support clustered database. If want to use high-available database in kube-ovn,
modify ovn-central deployment in yamls/ovn.yaml.

Change the replicas to 3, and add NODE_IPS environment var points to node that has label `kube-ovn/role: "master"`.
```yaml
      replicas: 3
      containers:
        - name: ovn-central
          image: "kubeovn/kube-ovn:v1.3.0"
          imagePullPolicy: Always
          env:
            - name: POD_IP
              valueFrom:
                fieldRef:
                  fieldPath: status.podIP
            - name: NODE_IPS
              value: 192.168.55.10, 192.168.55.11, 192.168.55.12
```

```bash
ovn-central-fbdbd9d4d-jv8cr            1/1       Running   0          19h      # waiting to become a leader
ovn-central-fbdbd9d4d-pgvhl            1/1       Running   0          19h      # waiting to become a leader
ovn-central-fbdbd9d4d-rk2c7            1/1       Running   0          19h      # the leader now
```

More detail about ovsdb cluster mode please refer to [this link](http://docs.openvswitch.org/en/latest/ref/ovsdb.7/#clustered-database-service-model)
