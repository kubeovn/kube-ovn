# Internal Port

Form 1.7.0, apart from the default veth-pair type network interface, Kube-OVN also provides ovs internal port type network interface which has better latency, throughput and cpu usages.

## How to use it?
### Installation options

You can set the interface type in `install.sh` scripts, by default it will use veth-pair.
```shell
POD_NIC_TYPE="internal-port"               # veth-pair or internal-port
```

You can also change the `kube-ovn-controller` args to use the new interface type
```yaml
containers:
        - name: kube-ovn-controller
          command:
          - /kube-ovn/start-controller.sh
          args:
         ...
          - --pod-nic-type=internal-port
```

### Pod options

You can set the interface type in Pod annotations to change the default interface type

```yaml
\apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/pod_nic_type: internal-port
```

## Some limitation
The internal port name should be unique on a host and kubelet always check the `eth0` interface in the Pod.

To bypass this issue, Kube-OVN creates a dummy type device in Pod netns with the same ip address of internal port and set the eth0 down. It works well for most scenarios, however if applications rely on network interface name, it will bring confusions.
