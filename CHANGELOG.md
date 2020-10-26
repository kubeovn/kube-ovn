# CHANGELOG

## 1.5.1 -- 2020/10/26

### New Feature
* Support binding pod to subnet

### Bugfix
* Remove not alive pod in networkpolicy portGroup
* Delete Pod when marked with deletionTimestamp
* Use internal IP when node try to connect to pod
* Do not advertise node switch cidr when enable ovn-ic
* Wrong proto str for udp diagnose
* Wrong ipv6 network format when update subnet
* Broken RPM link
* Default SSL var for compatibility
* Wrong iptable order
* Check multicast and loopback subnet
* CodeQL scan warnings

### Mics
* CI: change to official docker buildx action
* Perf: remove default acl rules
* Perf: accelerate ic and ex gw update

## 1.5.0 -- 2020/9/28

### New Feature
* Pod level SNAT and EIP support
* Integrate SFC function into OVN
* OVN-Controller graceful stop
* Mirror config can be updated dynamically
* Set more metadata to interface external-ids

### Security
* Support TLS connection between components
* Change DB file access mode

### Monitoring
* Add more metrics to pinger dashboard
* Add more metrics to kube-ovn-cni and a new Grafana dashboard
* Diagnose show ovn-nb and ovn-sb overview

### Mics
* Update CI k8s to 1.19
* Change kube-ovn-cni updateStrategy
* Move CNI conf when kube-ovn-cni ready

### Bugfix
* Use NodeName as OVN chassis name
* Stop OVN-IC if disabled
* Uninstall scripts will clean up ipv6 iptables and ipset
* Bridging-mapping may conflict, if enable vlan and external gateway 
* Pinger ipv6 mode fetch portmaping failed
* Pinger diagnose should reuse cmd args

## 1.4.0 -- 2020/9/1

### New Feature
* Integrate OVN-IC to support multi-cluster networking
* Enable ACL log to record networkpolicy drop packets
* Reserve source ip for NodePort service to local pod
* Support vlan subnet switch to underlay gateway

### Bugfix
* Add forward accept rules
* kubectl-ko cannot find nic
* Prevent vlan/subnet init error logs
* Subnet ACL might conflict if allSubnets and subnet cidr overlap
* Missing session lb

### Misc
* Update ovs to 2.14
* Update golang to 1.15
* Suppress logs
* Add psp rules
* Remove juju log dependency


## 1.3.0 -- 2020/7/31

### New Feature
* Hardware offload to boost performance in Bare-Metal environment
* Assigning a specific pod as gateway
* Support QoS of the central gateway 
* Session affinity service
* Round-robbin IP allocation to relieve IP conflict

### Security
* Use gosec to audit code security
* Use trivy to scan and fix image CVEs
* Update loopback plugin to fix CVEs

### Bugfix
* Missing package for arm images
* Node annotation overwrite incorrectly
* Create/Delete order might lead ip conflict
* Add MSS rules to resolve MTU issues

### Monitoring
* kubectl-ko support ovs-tracing
* Pinger support metrics to resolve external address

### Misc
* Update OVN to 20.06
* CRD version upgrade to v1
* Optimize ARM build
* Refactor ovs cmd with ovs.Exec
* OVS-DPDK support config file
* Add DPDK tools in OVS_DPDK image
* Reduce image size of OVS-DPDK

## v1.2.1 -- 2020/06/22
This release fix bugs found in v1.2.0

### Bugfix
* Add back privilege for IPv6
* Update loopback cni to fix CVE issues
* Node annotations overwrite incorrectly
* Create/Delete order might lead to ip conflict

## v1.2.0 -- 2020/05/30
In this version, Kube-OVN support vlan and dpdk type network interfaces for higher performance requirement.
Thanks for Intel and Ruijie Networks guys who contribute these features.

Previously to expose Pod IP to external network, admins have to manually add static routes.
Now admins can try the new BGP features to dynamically announce routes to external network.

From this version, subnet CIDR can be changed after creation, and routes will be changed if gateway type is modified.

### New Feature
* Kube-OVN now supports OVS-DPDK, high performance dpdk application can run in pod
* Kube-OVN now supports vlan underlay network to achieve better network performance
* Kube—OVN now supports using BGP to announce Pod IP routes to external network
* Subnet validator will check if subnet CIDR conflicts with svc or node CIDR
* Subnet CIDR can be changed after creation
* When subnet gateway changed, routes will aromatically changed


### Monitoring
* Check if dns and kubernetes svc exist
* Make grafana dashboard more sensitive to changes

### Misc
* Patch upstream ovn to reduce lflow count
* Add support for arm64 platform
* Add support for kubernetes 1.18
* Use github action to perform CI tasks
* Optimize some log information
* Move image to dockerhub


### Bugfix:
* OVS local interface table mac_in_use row is lower case, but pod annotation store mac in Upper case
* fork go-ping to fix ping lost issues
* Networkpolicy controller will panic if label is nil
* Some concurrent panic when handle pod and subnet update
* Some IPv6 break issues
* Use kubectl version to avoid handshake errors in apiserver

## v1.1.1 -- 2020/04/27

This release fix bugs found in v1.1.0.

### Bugfix
* Use legacy iptables to replace default iptables in centos:8 image
* Mount etc/origin/ovn to ovs-ovn
* Fix bugs in go-ping
* Fix yaml indent error
* Fix panic when handles networkpolicy

### Monitoring
* Make graph more sensitive to changes

## v1.1.0 -- 2020/04/07

In this version, we refactor IPAM to separate IP allocation logical from OVN.
On top of that we provide a general cluster wide IPAM utility for other CNI plugins.
Now other CNI plugins like macvlan/host-device/vlan etc can take advantage of subnet and static ip allocation functions in Kube-OVN.
Please check [this document](docs/multi-nic.md) to see how we combine Kube-OVN and Multus-CNI to provide multi-nic container network.

### IPAM
* Separate IPAM logical form OVN
* Add support for Multus-CNI

### Performance
* Recycle address if pod in failed or succeeded phase
* Delete chassis form ovn-sb when node deleted
* Only enqueue updatePod when needed
* Add x86 optimization CFLAGS
* Add support to disable encapsulation checksum

### Monitor
* Diagnose will check Kube-OVN components status
* Diagnose will check crd status
* Diagnose will check kube-proxy and coredns status

### Bugfix
* Use uuid to fetch the lb vips
* Add inactivity_probe back
* Update svc might remove other svc that with same prefix
* IP prefix might be empty
* Enqueue subnet update to add route
* Add iptables to accept container traffic

### Chore
* Update OVN to 20.03 and OVS to 2.13
* Add support for Kubernetes 1.17
* Put all component in one image to reduce distribute burden
* Add scripts to build ovs
* Add one script installer
* Add uninstall script
* Add more e2e tests

## v1.0.1 -- 2020/03/31

This release fix bugs found in v1.0.0

### Bugfix
* Use uuid to fetch the lb vips
* Add inactivity_probe back
* Update svc might remove other svc that with same prefix
* IP prefix might be empty
* Enqueue subnet update to add route

## v1.0.0 -- 2020/02/27

Kube-OVN has evolved a year from the first release and the core function set is stable with lots of tests and community feedback.
It's time to run Kube-OVN in production!

### Performance
* Disable ovn-nb inactivity_probe to enhance ovn-nbctl daemon performance
* Config ovn-openflow-probe-interval to prevent disconnection when cluster is large
* Pick ovn upstream patch to enhance ovn-controller performance

### Monitoring
* Display controller logs in kubectl-ko diagnose
* Expose cni operation metrics
* Pinger check portbindings between local ovs and ovs-sb
* Pinger add timeout for dns/ovs/ovn check

### Mics
* Add e2e test framework
* Move all components to kube-system namespace to use a higher priorityClass
* Refactor code for better readability

### Bugfix
* If cidr block not ends with zero, reformat the cidr block
* CniServer will resync iptables to avoid manually or other software change the iptable
* Do not return not found error when first add node
* Restart ovn-nbctl daemon when it hangs
* RunGateway will restart in case init failed.
* When subnet cidr conflict requeue the subnet
* Recompute ovn-controller periodically to avoid inconsistency
* Wait for flow installed before cni return
* Add back missing lsp gc
* Delete the lb if it has no backends

## v0.10.0 -- 2019/12/23

### Performance
* Update ovn to 2.12.0 and pick performance and raft bugfix from upstream
* Modify upstream ovn to reduce memory footprint
* CniServer filter pod in the informer list-watch and disable resync
* Skip evicted pod when enqueueAddPod and enqueueUpdatePod
* When controller restart skip pod already create lsp
* As lr-route-add with --may-exist will replace exist route, no need for another delete

### Monitoring
* Pinger support to check external address

### Bugfix
* When all ip in a subnet is used up, creating lsp will panic with an index out of range err
* Mount /var/run/netns into kube-ovn-cniserver for kind
* Use ep.subset.port.name to infer target port number
* Typo in start-ovs.sh
* When delete node recycle related ip/route resource
* Nbctl need timeout to avoid the infinitely hang
* Block subnet deletion when there is any ip in use
* IP conflict when use ippool
* GC logical_switch_port form listing pods and nodes
* Do not add unallocated pod to port-group
* PodSelector in networkpolicy should only consider pods in the same ns

### Mics
* Support kind installation
* Use label to select leader to avoid pod status misleading
* Add wait in cniserver and controller to reduce errors and restarts

## v0.9.1 -- 2019/12/02

This release fix bugs found in v0.9.0

### Bugfix
* When all ip in a subnet is used up, create lsp will panic with an index out of range err
* Mount /var/run/netns into kube-ovn-cniserver for kind
* Use ep.subset.port.name to infer target port number
* Typo in start-ovs.sh
* When delete node recycle related ip/route resource
* Nbctl need timeout to avoid the infinitely hang
* Block subnet deletion when there any ip in use

## v0.9.0 -- 2019/11/21

This release is mainly about controller performance, stability and bugfix

### Monitoring
* Improve kube-ovn-pinger metrics to check apiserver and dns
* Add kube-ovn-controller metrics to show the controller status
* Add grafana templates to visualize metrics

### Performance
* Adjust client-go param to increase parallelism
* Adjust ovn-db and ovn-controller resource
* Merge some ovn-nb requests and remove most wait=ovn-nb params

### Stability and Bugfix
* LB init conflict when use multiple kube-ovn-controller
* Static Route might lost during leader election
* If pod have not a status.PodIP skip add/del static route
* Add keepalive to ovn-controller
* Add qlen when set egress QoS
* Add ingress_policing_burst to accurate limit ingress bandwidth
* GC resources when kube-ovn-controller starts
* Re-annotate related namespaces when subnet deleted.
* Check the short name of kubernetes services which is independent of the cluster domain name
* Daemonset updateStrategy changes to OnDelete for grace update
* Use new upstream ovn with some kube-ovn related modification

### Misc
* Remove most privilege container
* When use nodelocaldns, do not nat the local dns server ip

## v0.8.0 -- 2019/10/08

### Gateway
* Support active-backup mode centralized gateway high available

### Diagnose Tools
* Kubectl plugin to trace/tcpdump/diagnose pod network traffic
* Pinger to test cluster network quality and expose metrics to Prometheus

### IPAM
* Join subnet ip now can be displayed by `kubectl get ip`

### Security
* Enable port security to prevent Mac and IP spoofing
* Allow nodes to pods traffic for private subnet

### Mics
* Support hostport
* Update OVN/OVS to 2.11.3
* Update Go to 1.13

## v0.7.0 -- 2019/08/21

### IPAM
* Reserve vNic for statefulset pods, statefulset pod will reuse previous nic info during statefulset lifetime
* New IP CRD, now you can use `kubectl get ip` to obtain ip allocation info

### Subnet
* Check logical switch existence before related operations
* Calculate default values for custom subnet
* Auto unbinds the previous subnet when namespace bind to a new subnet
* Subnet CRD now has status field to show ip allocation statistic and subnet condition
* Write subnet annotations back to bind namespace

### Security
* Enable traffic mirror by default
* Support select all type NetworkPolicy rules
* Private subnets now apply acl to all ports not only gateway ports

### IPv6
* Control plan components now can communicate with IPv6 protocol

### Misc
* New logo
* [中文文档](https://github.com/alauda/kube-ovn/wiki)
* Test Kube-OVN compatible on CentOS 7.5/Ubuntu 16.04 and Ubuntu 18.04
* Add support for Kubespray and kubeasz installation tools
* Rename cni conf to `00-kube-ovn.conflist` to improve kubelet priority
* Basic TCP [performance test](https://github.com/alauda/kube-ovn/wiki/%E9%98%BF%E9%87%8C%E4%BA%91%E6%B5%8B%E8%AF%95) on Aliyun.

## v0.6.0 -- 2019/07/22
### Features
* Support traffic mirror
* Use webhook to check ip conflict
* Beta IPv6 support
* Use subnet CRD to replace namespace annotation
* Use go mod to manage dependency

### Bug fixes
* Remove RBAC dependency on cluster-admin
* Use kubernetes nodename to replace hostname

## v0.5.0 -- 2019/06/06
### Features
* Support NetworkPolicy by OVN ACL
* User can choose the interface for inter-host communication
* User can set mtu of pod interface
* Set kernel args when start cniserver
* Add pprof and use it as liveness/readiness probe
* Assign default gw for default switch and node switch
* Expose more cmd args to configure controller and daemon

### Misc
* Remove mask field from ip annotation

## v0.4.1 -- 2019/05/27
This is a bugfix version

### Bug Fixes
* manual static ip allocation and automatic allocation should use different ip validation
* json: cannot unmarshal string into Go value of type request.PodResponse
* use ovsdb-client to get leader info to avoid log rotation
* use default-gw as default-exclude-ips and expose args to docs
* to clean up all created resources, not only kube-ovn namespace.

## v0.4.0 -- 2019/05/16
### Features
* ovndb now support cluster ha mode
* kube-ovn-controller now support ha mode by leader election
* Pod IP can be exposed to external network directly
* Update OVN to 2.11.1 to fix some known bugs
* Parallelize kube-ovn process to improve control plane performance
* Add vagrant files to do e2e tests
* Use ovs-ctl and ovn-ctl to do health check
### Bug Fixes
* Check subnet cidr conflict
* Validate namespace and pod annotations
* Daemon wait for node annotations ready
* Reuse node annotations when kube-ovn-controller restart

## v0.3.0 -- 2019/04/19
### Features
* Namespaced Gateway for external connectivity
* Daemon ovn-nbctl to improve performance
### Fix
* Daemon init node gw before running controller
* Activate node switch by ping
* Fix ovn-nbctl daemon output format bugs
* ACL allow error

## v0.2.0 -- 2019/04/15
### Features
* Distributed Gateway for external connectivity
* Dynamic QoS for pod ingress/egress bandwidth
* Subnet isolation
### Bug Fixes
* Delete empty lb to improve performance
* Delete lb at node switch
* Delete ovn embedded dns
* Fix ovn restart failed issue


## v0.1.0 -- 2019/03/12
### Features
* IP/Mac automatic allocation
* IP/Mac static allocation
* Namespace bind subnet
* Namespaces share subnet
* Connectivity between nodes and pods
### Issues
* Pod cannot access external network
* No HA for control plan
