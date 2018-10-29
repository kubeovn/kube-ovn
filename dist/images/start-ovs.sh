#!/bin/bash
set -euo pipefail

# Install the kernel mod for kernel datapath
rpm -i /root/kmod.rpm
modprobe vport-geneve

/usr/share/openvswitch/scripts/ovs-ctl start --no-ovs-vswitchd --system-id=random
# Restrict the number of pthreads ovs-vswitchd creates to reduce the
# amount of RSS it uses on hosts with many cores
# https://bugzilla.redhat.com/show_bug.cgi?id=1571379
# https://bugzilla.redhat.com/show_bug.cgi?id=1572797
if [[ `nproc` -gt 12 ]]; then
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-revalidator-threads=4
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-handler-threads=10
fi

/usr/share/openvswitch/scripts/ovs-ctl start --no-ovsdb-server  --system-id=random
/usr/share/openvswitch/scripts/ovs-ctl --protocol=udp --dport=6081 enable-protocol

function quit {
	/usr/share/openvswitch/scripts/ovs-ctl stop
	exit 0
}
trap quit SIGTERM

tail -f /var/log/openvswitch/ovs-vswitchd.log
