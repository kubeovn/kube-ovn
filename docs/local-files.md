# Local Files

Kube-OVN will store files to the host filesystem. Administrators should take care of these locations to avoid malicious application access to them or accidentally delete them.

* `/etc/origin/ovn` and `/etc/origin/openvswitch` store the OVN and OVS db files. These files store the logical network topology.
* `/var/run/ovn` and `/var/run/openvswitch` store the OVN and OVS socket files. These files provide endpoints to OVN/OVS control plane.
* `/var/log/ovn` and `/var/log/openvswitch` store the log files of OVN and OVS.
* `/etc/cni/net.d` stores the Kube-OVN CNI conf.
* `/etc/cni/bin` stores the Kube-OVN CNI binary. 
