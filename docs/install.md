# Installation

Kube-OVN includes two parts:
- Native OVS and OVN components
- Controller and CNI plugins that integrate OVN with Kubernetes

## Prerequisite
- Kubernetes >= 1.16
- Docker >= 1.12.6
- OS: CentOS 7/8, Ubuntu 16.04/18.04 
- Other Linux distributions with geneve, openvswitch and ip_tables module installed. You can use commands  `modinfo geneve`, `modinfo openvswitch` and `modinfo ip_tables` to verify
- Kernel boot with `ipv6.disable=0`
- Kube-proxy *MUST* be ready so that Kube-OVN can connect to apiserver

*NOTE*
1. Users using Ubuntu 16.04 should build the OVS kernel module and replace the built-in one to avoid kernel NAT issues.
2. CentOS users should make sure kernel version is greater than 3.10.0-898 to avoid a kernel conntrack bug, see [here](https://bugs.launchpad.net/neutron/+bug/1776778).
3. Kernel must boot with IPv6 enabled, otherwise geneve tunnel will not be established due to a kernel bug, see [here](https://bugs.launchpad.net/ubuntu/+source/linux/+bug/1794232).

## To Install

### One Script Installer

Kube-OVN provides a one script install to easily install a high-available, production-ready Kube-OVN.

1. Download the stable release installer scripts.

For Kubernetes version>=1.16:
`wget https://raw.githubusercontent.com/kubeovn/kube-ovn/release-1.9/dist/images/install.sh`

If you want to try the latest developing Kube-OVN, try the script below:
`wget https://raw.githubusercontent.com/kubeovn/kube-ovn/master/dist/images/install.sh`

2. Use vim to edit the script variables to meet your requirement:
```bash
 REGISTRY="kubeovn"
 POD_CIDR="10.16.0.0/16"                # Default subnet CIDR, Do NOT overlap with NODE/SVC/JOIN CIDR
 SVC_CIDR="10.96.0.0/12"                # Should be equal with service-cluster-ip-range CIDR range which is configured for the API server
 JOIN_CIDR="100.64.0.0/16"              # Subnet CIDR used for connectivity between nodes and Pods, Do NOT overlap with NODE/POD/SVC CIDR
 LABEL="node-role.kubernetes.io/master" # The node label to deploy OVN DB
 IFACE=""                               # The nic to support container network can be a nic name or a group of regex separated by comma e.g. `IFACE=enp6s0f0,eth.*`, if empty will use the nic that the default route use
 VERSION="v1.9.0"
```

This basic setup works for default overlay network. If you are using default underlay/vlan network, please refer [Vlan/Underlay Support](vlan.md).

3. Run the script

`bash install.sh`

That's all! You can now create some pods and test connectivity.

### Step by Step Install

The one-script installer is recommended. If you want to change the default options, follow the steps below.

For Kubernetes version before 1.17 please use the following command to add the node label:

    `kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite`

1. Add the following label to the Node which will host the OVN DB and the OVN Control Plane:

    `kubectl label node <Node on which to deploy OVN DB> kube-ovn/role=master`
2. Install Kube-OVN related CRDs:

    `kubectl apply -f https://raw.githubusercontent.com/kubeovn/kube-ovn/release-1.9/yamls/crd.yaml`
3. Get ovn.yaml and replace `$addresses` in the file with IP address of the node that will host the OVN DB and the OVN Control Plane:

    `curl -O https://raw.githubusercontent.com/kubeovn/kube-ovn/release-1.9/yamls/ovn.yaml`

    `sed -i 's/\$addresses/<Node IP>/g' ovn.yml`
4. Install native OVS and OVN components:

    `kubectl apply -f ovn.yaml`
5. Install the Kube-OVN Controller and CNI plugins:

    `kubectl apply -f https://raw.githubusercontent.com/kubeovn/kube-ovn/release-1.9/yamls/kube-ovn.yaml`

For high-available ovn db, see [High Availability](high-availability.md).

If you want to enable IPv6 on default subnet and node subnet, please apply https://raw.githubusercontent.com/kubeovn/kube-ovn/release-1.9/yamls/kube-ovn-ipv6.yaml on Step 3.

## More Configuration

Kube-OVN will use subnet to manage pod ip address allocation, so the kube-controller-manager flag `cluster-cidr` will not take effect.
You can use `--default-cidr` flags below to config default Pod CIDR or create a new subnet with desired CIDR later.

### Controller Configuration

```text
      --add_dir_header                            If true, adds the file directory to the header
      --alsologtostderr                           log to standard error as well as files
      --cluster-router string                     The router name for cluster router (default "ovn-cluster")
      --cluster-tcp-loadbalancer string           The name for cluster tcp loadbalancer (default "cluster-tcp-loadbalancer")
      --cluster-tcp-session-loadbalancer string   The name for cluster tcp session loadbalancer (default "cluster-tcp-session-loadbalancer")
      --cluster-udp-loadbalancer string           The name for cluster udp loadbalancer (default "cluster-udp-loadbalancer")
      --cluster-udp-session-loadbalancer string   The name for cluster udp session loadbalancer (default "cluster-udp-session-loadbalancer")
      --default-cidr string                       Default CIDR for namespace with no logical switch annotation (default "10.16.0.0/16")
      --default-exclude-ips string                Exclude ips in default switch (default gateway address)
      --default-gateway string                    Default gateway for default-cidr (default the first ip in default-cidr)
      --default-gateway-check                     Check switch for the default subnet's gateway (default true)
      --default-interface-name string             The default host interface name in the vlan/vxlan type
      --default-logical-gateway                   Create a logical gateway for the default subnet instead of using underlay gateway. Take effect only when the default subnet is in underlay mode. (default false)
      --default-ls string                         The default logical switch name (default "ovn-default")
      --default-provider-name string              The vlan or vxlan type default provider interface name (default "provider")
      --default-vlan-id int                       The default vlan id (default 1)
      --default-vlan-name string                  The default vlan name (default "ovn-vlan")
      --enable-external-vpc                       Enable external vpc support (default true)
      --enable-lb                                 Enable load balancer (default true)
      --enable-np                                 Enable network policy support (default true)
      --kubeconfig string                         Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.
      --log_backtrace_at traceLocation            when logging hits line file:N, emit a stack trace (default :0)
      --log_dir string                            If non-empty, write log files in this directory
      --log_file string                           If non-empty, use this log file
      --log_file_max_size uint                    Defines the maximum size a log file can grow to. Unit is megabytes. If the value is 0, the maximum file size is unlimited. (default 1800)
      --logtostderr                               log to standard error instead of files (default true)
      --multicast-privileged                      Move broadcast/multicast flows to table ls_in_pre_lb in logical switches' ingress pipeline to improve broadcast/multicast performace (default false)
      --network-type string                       The ovn network type (default "geneve")
      --node-switch string                        The name of node gateway switch which help node to access pod network (default "join")
      --node-switch-cidr string                   The cidr for node switch (default "100.64.0.0/16")
      --node-switch-gateway string                The gateway for node switch (default the first ip in node-switch-cidr)
      --ovn-nb-addr string                        ovn-nb address
      --ovn-sb-addr string                        ovn-sb address
      --ovn-timeout int                            (default 60)
      --pod-nic-type string                       The default pod network nic implementation type (default "veth-pair")
      --pprof-port int                            The port to get profiling data (default 10660)
      --service-cluster-ip-range string           The kubernetes service cluster ip range (default "10.96.0.0/12")
      --skip_headers                              If true, avoid header prefixes in the log messages
      --skip_log_headers                          If true, avoid headers when opening log files
      --stderrthreshold severity                  logs at or above this threshold go to stderr (default 2)
  -v, --v Level                                   number for the log level verbosity
      --vmodule moduleSpec                        comma-separated list of pattern=N settings for file-filtered logging
      --worker-num int                            The parallelism of each worker (default 3)
```

### Daemon Configuration

```text
      --add_dir_header                    If true, adds the file directory to the header
      --alsologtostderr                   log to standard error as well as files
      --bind-socket string                The socket daemon bind to. (default "/run/openvswitch/kube-ovn-daemon.sock")
      --default-interface-name string     The default host interface name in the vlan/vxlan type
      --default-provider-name string      The vlan or vxlan type default provider interface name (default "provider")
      --enable-mirror                     Enable traffic mirror (default false)
      --encap-checksum                    Enable checksum (default true)
      --iface string                      The iface used to inter-host pod communication, can be a nic name or a group of regex separated by comma (default the default route iface)
      --kubeconfig string                 Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.
      --log_backtrace_at traceLocation    when logging hits line file:N, emit a stack trace (default :0)
      --log_dir string                    If non-empty, write log files in this directory
      --log_file string                   If non-empty, use this log file
      --log_file_max_size uint            Defines the maximum size a log file can grow to. Unit is megabytes. If the value is 0, the maximum file size is unlimited. (default 1800)
      --logtostderr                       log to standard error instead of files (default true)
      --mirror-iface string               The mirror nic name that will be created by kube-ovn (default "mirror0")
      --mtu int                           The MTU used by pod iface in overlay networks (default iface MTU - 100)
      --network-type string               The ovn network type (default "geneve")
      --node-local-dns-ip string          If use nodelocaldns the local dns server ip should be set here.
      --ovs-socket string                 The socket to local ovs-server
      --pprof-port int                    The port to get profiling data (default 10665)
      --service-cluster-ip-range string   The kubernetes service cluster ip range (default "10.96.0.0/12")
      --skip_headers                      If true, avoid header prefixes in the log messages
      --skip_log_headers                  If true, avoid headers when opening log files
      --stderrthreshold severity          logs at or above this threshold go to stderr (default 2)
  -v, --v Level                           number for the log level verbosity
      --vmodule moduleSpec                comma-separated list of pattern=N settings for file-filtered logging
```

### Install with customized kubeconfig

By default, Kube-OVN uses in-cluster config to init kube client. In this way, Kube-OVN relies on kube-proxy to provide service discovery to connect to Kubernetes apiserver. 
To use an external or high available Kubernetes apiserver, users can use self customized kubeconfig to connect to apiserver.

1. Generate configmap from an existing kubeconfig:

```bash
kubectl create -n kube-system configmap admin-conf --from-file=config=admin.conf
```

1. Edit `kube-ovn-controller`, `kube-ovn-cni` to use the above kubeconfig:

```yaml
      - args:
           - --kubeconfig=/root/.kube/config

        ...

        volumeMounts:
           - mountPath: /root/.kube
             name: kubeconfig
        volumes:
           - configMap:
                defaultMode: 420
                name: admin-conf
             name: kubeconfig
```

## To uninstall

1. Remove Kubernetes resources:

 ```bash
 wget https://raw.githubusercontent.com/kubeovn/kube-ovn/release-1.9/dist/images/cleanup.sh
 bash cleanup.sh
 ```

2. Delete OVN/OVS DB and config files on every Node:

 ```bash
 rm -rf /var/run/openvswitch
 rm -rf /var/run/ovn
 rm -rf /etc/origin/openvswitch/
 rm -rf /etc/origin/ovn/
 # default value
 rm -rf /etc/cni/net.d/01-kube-ovn.conflist
 rm -rf /var/log/openvswitch
 rm -rf /var/log/ovn
 ```

3. Reboot the Node to remove ipset/iptables rules and nics.
