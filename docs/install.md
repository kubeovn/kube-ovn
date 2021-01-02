# Installation

Kube-OVN includes two parts:
- Native OVS and OVN components
- Controller and CNI plugins that integrate OVN with Kubernetes

## Prerequest
- Kubernetes >= 1.11
- Docker >= 1.12.6
- OS: CentOS 7.5/7.6/7.7, Ubuntu 16.04/18.04
- Kernel boot with `ipv6.disable=0`

*NOTE*
1. Ubuntu 16.04 users should build the related ovs-2.11.1 kernel module to replace the kernel built-in module
2. CentOS users should make sure kernel version is greater than 3.10.0-898 to avoid a kernel conntrack bug, see [here](https://bugs.launchpad.net/neutron/+bug/1776778)
3. Kernel must boot with ipv6 enabled, otherwise geneve tunnel will not be established due to a kernel bug, see [here](https://bugs.launchpad.net/ubuntu/+source/linux/+bug/1794232)

## To Install

### One Script Installer

Kube-OVN provides a one script install to easily install a high-available, production-ready Kube-OVN

1. Download the stable release installer scripts

For Kubernetes version>=1.16
`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.5/dist/images/install.sh`

For Kubernetes version<1.16
`wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.5/dist/images/install-pre-1.16.sh`

If you want to try the latest developing Kube-OVN, try the script below
`wget https://raw.githubusercontent.com/alauda/kube-ovn/master/dist/images/install.sh`

2. Use vim to edit the script variables to meet your requirement
```bash
 REGISTRY="kubeovn"
 POD_CIDR="10.16.0.0/16"                # Do NOT overlap with NODE/SVC/JOIN CIDR
 SVC_CIDR="10.96.0.0/12"                # Do NOT overlap with NODE/POD/JOIN CIDR
 JOIN_CIDR="100.64.0.0/16"              # Do NOT overlap with NODE/POD/SVC CIDR
 LABEL="node-role.kubernetes.io/master" # The node label to deploy OVN DB
 IFACE=""                               # The nic to support container network can be a nic name or a group of regex separated by comma, if empty will use the nic that the default route use
 VERSION="v1.5.2"
```

After v1.6.0 `IFACE` support regex, e.g. `IFACE=enp6s0f0,eth.*`

3. Execute the script

`bash install.sh`

### Step by Step Install

If you want to know the detail steps to install Kube-OVN, please follow the steps.

For Kubernetes version before 1.17 please use the following command to add the node label

    `kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite`

1. Add the following label to the Node which will host the OVN DB and the OVN Control Plane:

    `kubectl label node <Node on which to deploy OVN DB> kube-ovn/role=master`
2. Install Kube-OVN related CRDs

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/release-1.5/yamls/crd.yaml`
3. Install native OVS and OVN components:

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/release-1.5/yamls/ovn.yaml`
4. Install the Kube-OVN Controller and CNI plugins:

    `kubectl apply -f https://raw.githubusercontent.com/alauda/kube-ovn/release-1.5/yamls/kube-ovn.yaml`

That's all! You can now create some pods and test connectivity.

For high-available ovn db, see [high available](high-available.md)

If you want to enable IPv6 on default subnet and node subnet, please apply https://raw.githubusercontent.com/alauda/kube-ovn/release-1.5/yamls/kube-ovn-ipv6.yaml on Step 3.

## More Configuration

Kube-OVN will use subnet to manage pod ip address allocation, so the kube-controller-manager flag `cluster-cidr` will not take effect.
You can use `--default-cidr` flags below to config default Pod CIDR or create a new subnet with desired CIDR later.

### Controller Configuration

```bash
      --add_dir_header                            If true, adds the file directory to the header
      --alsologtostderr                           log to standard error as well as files
      --cluster-router string                     The router name for cluster router, default: ovn-cluster (default "ovn-cluster")
      --cluster-tcp-loadbalancer string           The name for cluster tcp loadbalancer (default "cluster-tcp-loadbalancer")
      --cluster-tcp-session-loadbalancer string   The name for cluster tcp session loadbalancer (default "cluster-tcp-session-loadbalancer")
      --cluster-udp-loadbalancer string           The name for cluster udp loadbalancer (default "cluster-udp-loadbalancer")
      --cluster-udp-session-loadbalancer string   The name for cluster udp session loadbalancer (default "cluster-udp-session-loadbalancer")
      --default-cidr string                       Default CIDR for namespace with no logical switch annotation, default: 10.16.0.0/16 (default "10.16.0.0/16")
      --default-exclude-ips string                Exclude ips in default switch, default equals to gateway address
      --default-gateway string                    Default gateway for default-cidr, default the first ip in default-cidr
      --default-interface-name string             The default host interface name in the vlan/xvlan type
      --default-ls string                         The default logical switch name, default: ovn-default (default "ovn-default")
      --default-provider-name string              The vlan or xvlan type default provider interface name, default: provider (default "provider")
      --default-vlan-id int                       The default vlan id, default: 1 (default 1)
      --default-vlan-name string                  The default vlan name, default: ovn-vlan (default "ovn-vlan")
      --default-vlan-range string                 The default vlan range, default: 1-4095 (default "1,4095")
      --kubeconfig string                         Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.
      --log_backtrace_at traceLocation            when logging hits line file:N, emit a stack trace (default :0)
      --log_dir string                            If non-empty, write log files in this directory
      --log_file string                           If non-empty, use this log file
      --log_file_max_size uint                    Defines the maximum size a log file can grow to. Unit is megabytes. If the value is 0, the maximum file size is unlimited. (default 1800)
      --logtostderr                               log to standard error instead of files (default true)
      --network-type string                       The ovn network type, default: geneve (default "geneve")
      --node-switch string                        The name of node gateway switch which help node to access pod network, default: join (default "join")
      --node-switch-cidr string                   The cidr for node switch, default: 100.64.0.0/16 (default "100.64.0.0/16")
      --node-switch-gateway string                The gateway for node switch, default the first ip in node-switch-cidr
      --ovn-nb-addr string                        ovn-nb address
      --ovn-sb-addr string                        ovn-sb address
      --ovn-timeout int                            (default 30)
      --pprof-port int                            The port to get profiling data, default 10660 (default 10660)
      --skip_headers                              If true, avoid header prefixes in the log messages
      --skip_log_headers                          If true, avoid headers when opening log files
      --stderrthreshold severity                  logs at or above this threshold go to stderr (default 2)
  -v, --v Level                                   number for the log level verbosity
      --vmodule moduleSpec                        comma-separated list of pattern=N settings for file-filtered logging
      --worker-num int                            The parallelism of each worker, default: 3 (default 3)
```

### Daemon Configuration

```bash
      --add_dir_header                    If true, adds the file directory to the header
      --alsologtostderr                   log to standard error as well as files
      --bind-socket string                The socket daemon bind to. (default "/var/run/cniserver.sock")
      --default-interface-name string     The default host interface name in the vlan/xvlan type
      --default-provider-name string      The vlan or xvlan type default provider interface name, default: provider (default "provider")
      --enable-mirror                     Enable traffic mirror, default: false
      --encap-checksum                    Enable checksum, default: true (default true)
      --iface string                      The iface used to inter-host pod communication, can be a nic name or a group of regex separated by comma, default: the default route iface
      --kubeconfig string                 Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.
      --log_backtrace_at traceLocation    when logging hits line file:N, emit a stack trace (default :0)
      --log_dir string                    If non-empty, write log files in this directory
      --log_file string                   If non-empty, use this log file
      --log_file_max_size uint            Defines the maximum size a log file can grow to. Unit is megabytes. If the value is 0, the maximum file size is unlimited. (default 1800)
      --logtostderr                       log to standard error instead of files (default true)
      --mirror-iface string               The mirror nic name that will be created by kube-ovn, default: mirror0 (default "mirror0")
      --mtu int                           The MTU used by pod iface, default: iface MTU - 100
      --network-type string               The ovn network type, default: geneve (default "geneve")
      --node-local-dns-ip string          If use nodelocaldns the local dns server ip should be set here, default empty.
      --ovs-socket string                 The socket to local ovs-server
      --pprof-port int                    The port to get profiling data, default: 10665 (default 10665)
      --service-cluster-ip-range string   The kubernetes service cluster ip range, default: 10.96.0.0/12 (default "10.96.0.0/12")
      --skip_headers                      If true, avoid header prefixes in the log messages
      --skip_log_headers                  If true, avoid headers when opening log files
      --stderrthreshold severity          logs at or above this threshold go to stderr (default 2)
  -v, --v Level                           number for the log level verbosity
      --vmodule moduleSpec                comma-separated list of pattern=N settings for file-filtered logging
```

## To uninstall

1. Remove Kubernetes resources:

    ```bash
    wget https://raw.githubusercontent.com/alauda/kube-ovn/release-1.5/dist/images/cleanup.sh
    bash cleanup.sh
    ```

2. Delete OVN/OVS DB and config files on every Node:

    ```bash
    rm -rf /var/run/openvswitch
    rm -rf /var/run/ovn
    rm -rf /etc/origin/openvswitch/
    rm -rf /etc/origin/ovn/
    rm -rf /etc/cni/net.d/00-kube-ovn.conflist
    rm -rf /etc/cni/net.d/01-kube-ovn.conflist
    rm -rf /var/log/openvswitch
    rm -rf /var/log/ovn
    ```
3. Reboot the Node to remove ipset/iptables rules and nics.
