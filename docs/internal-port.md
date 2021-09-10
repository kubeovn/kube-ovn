# Internal Port

Form 1.7.0, apart from the default veth-pair type network interface, Kube-OVN also provides ovs internal port type network interface which has better latency, throughput and cpu usages.

## How to use it?
### Installation options

You can set the interface type in `install.sh` scripts, by default it will use veth-pair.

```bash
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
apiVersion: v1
kind: Pod
metadata:
  annotations:
    ovn.kubernetes.io/pod_nic_type: internal-port
```

## Some limitation
- The internal port name must be unique on a host and kubelet always checks the `eth0` interface in the Pod. To bypass this issue, Kube-OVN creates a dummy interface named eth0 in the Pod's netns, assigns the same IP address(es), and sets it down. It works well for most scenarios, however if applications rely on network interface name, it will bring confusions.
- After OVS restarts, internal ports will be detached from the pod. Pods on the same node with internal-port interfaces should be recreated manually.
