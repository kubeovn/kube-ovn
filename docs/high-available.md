# High available for ovn db

OVN support clustered database. If want to use high-available database in kube-ovn,
modify ovn-central deployment in yamls/ovn.yaml.

Change the replicas to 3, and add NODE_IPS environment var points to node that has label `kube-ovn/role: "master"`.
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

When using cluster mode, only the leader ovsdb pod will be ready and serve requests, other pod will be waiting to become a leader.

```bash
ovn-central-fbdbd9d4d-jv8cr            0/1       Running   0          19h      # waiting to become a leader
ovn-central-fbdbd9d4d-pgvhl            0/1       Running   0          19h      # waiting to become a leader
ovn-central-fbdbd9d4d-rk2c7            1/1       Running   0          19h      # the leader now
```

More detail about ovsdb cluster mode please refer to [this link](http://docs.openvswitch.org/en/latest/ref/ovsdb.7/#clustered-database-service-model)