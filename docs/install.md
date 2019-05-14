# Installation


Kube-OVN includes two parts:
- Native OVS and OVN components
- Controller and CNI plugins that integrate OVN with Kubernetes

## To install

1. Add the following label to the Node which will host the OVN DB and the OVN Control Plane:

    `kubectl label node <Node on which to deploy OVN DB> kube-ovn/role=master`
2. Install native OVS and OVN components:

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/v0.3.0/yamls/ovn.yaml`
3. Install the Kube-OVN Controller and CNI plugins:

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/v0.3.0/yamls/kube-ovn.yaml`

That's all! You can now create some pods and test connectivity.

For high-available ovn db, see [high available](high-available.md)

## More Configuration

### Controller Configuration

```bash
    --default-cidr: Default CIDR for Namespaces with no logical switch annotation, default: 10.16.0.0/16
    --node-switch-cidr: The CIDR for the Node switch. Default: 100.64.0.0/16
```

## To uninstall

1. Remove Kubernetes resources:

    ```bash
    wget https://raw.githubusercontent.com/alauda/kube-ovn/master/dist/images/cleanup.sh
    bash cleanup.sh
    ```

2. Delete OVN/OVS DB and config files on every Node:

    ```bash
    rm -rf /var/run/openvswitch
    rm -rf /etc/origin/openvswitch/
    rm -rf /etc/openvswitch
    ```
3. Reboot the Node to remove ipset/iptables rules and nics.