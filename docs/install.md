# Installation


Kube-OVN includes two parts:
- Native OVS and OVN components
- Controller and CNI plugins that integrate OVN with Kubernetes

## Prerequest
- Kubernetes >= 1.11
- Docker >= 1.12.6
- OS: CentOS 7.5, Ubuntu 16.04/18.04

*NOTE* 
1. Ubuntu 16.04 users should build the related ovs-2.11.1 kernel module to replace the kernel built-in module 
2. CentOS users should make sure kernel version is greater than 3.10.0-898 to avoid a kernel conntrack bug, see [here](https://bugs.launchpad.net/neutron/+bug/1776778) 

## To Install

1. Add the following label to the Node which will host the OVN DB and the OVN Control Plane:

    `kubectl label node <Node on which to deploy OVN DB> kube-ovn/role=master`
2. Install Kube-OVN related CRDs

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/v0.10.2/yamls/crd.yaml`
3. Install native OVS and OVN components:

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/v0.10.2/yamls/ovn.yaml`
4. Install the Kube-OVN Controller and CNI plugins:

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/v0.10.2/yamls/kube-ovn.yaml`
    
That's all! You can now create some pods and test connectivity.

For high-available ovn db, see [high available](high-available.md)

If you want to enable IPv6 on default subnet and node subnet, please apply https://raw.githubusercontent.com/alauda/kube-ovn/v0.10.2/yamls/kube-ovn-ipv6.yaml on Step 3.

## More Configuration

Kube-OVN will use subnet to manage pod ip address allocation, so the kube-controller-manager flag `cluster-cidr` will not take effect.
You can use `--default-cidr` flags below to config default Pod CIDR or create a new subnet with desired CIDR later.

### Controller Configuration

```bash
    # Default Logical Switch
    --default-ls: The default logical switch name, default: ovn-default
    --default-cidr: Default CIDR for Namespaces with no logical switch annotation, default: 10.16.0.0/16
    --default-gateway: Default gateway for default-cidr, default the first ip in default-cidr
    --default-exclude-ips: Exclude ips in default switch, default equals to gateway address
    
    # Node Switch
    --node-switch: The name of node gateway switch which help node to access pod network, default: join
    --node-switch-cidr: The cidr for node switch, default: 100.64.0.0/16
    --node-switch-gateway: The gateway for node switch, default the first ip in node-switch-cidr
    
    # LoadBalancer
    --cluster-tcp-loadbalancer: The name for cluster tcp loadbalancer, default cluster-tcp-loadbalancer
    --cluster-udp-loadbalancer: The name for cluster udp loadbalancer, default cluster-udp-loadbalancer
    
    # Router
    --cluster-router: The router name for cluster router, default: ovn-cluster
    
    # Misc
    --worker-num: The parallelism of each worker, default: 3
    --kubeconfig: Path to kubeconfig file with authorization and master location information. If not set use the inCluster token
```

### Daemon Configuration

```bash
    --iface: The iface used to inter-host pod communication, default: the default route iface
    --mtu: The MTU used by pod iface, default: iface MTU - 100
    --enable-mirror: Enable traffic mirror, default: false
    --mirror-iface: The mirror nic name that will be created by kube-ovn, default: mirror0
    --service-cluster-ip-range: The kubernetes service cluster ip range, default: 10.96.0.0/12
    --kubeconfig: Path to kubeconfig file with authorization and master location information. If not set use the inCluster token
```

## To uninstall

1. Remove Kubernetes resources:

    ```bash
    wget https://raw.githubusercontent.com/alauda/kube-ovn/v0.10.2/dist/images/cleanup.sh
    bash cleanup.sh
    ```

2. Delete OVN/OVS DB and config files on every Node:

    ```bash
    rm -rf /var/run/openvswitch
    rm -rf /etc/origin/openvswitch/
    rm -rf /etc/openvswitch
    rm -rf /etc/cni/net.d/00-kube-ovn.conflist
    rm -rf /var/log/openvswitch
    ```
3. Reboot the Node to remove ipset/iptables rules and nics.
