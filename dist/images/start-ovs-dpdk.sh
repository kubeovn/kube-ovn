#!/bin/bash

function quit {
	/usr/share/openvswitch/scripts/ovs-ctl stop
	/usr/share/openvswitch/scripts/ovn-ctl stop_controller
	/usr/share/openvswitch/scripts/ovn-ctl stop_controller_vtep
	exit 0
}
trap quit EXIT

# Start ovsdb
/usr/share/openvswitch/scripts/ovs-ctl restart --no-ovsdb-server  --system-id=random
/usr/share/openvswitch/scripts/ovs-ctl --protocol=udp --dport=6081 enable-protocol

/usr/share/openvswitch/scripts/ovs-ctl --no-ovs-vswitchd start
/usr/share/openvswitch/scripts/ovs-vsctl --no-wait set Open_vSwitch . other_config:dpdk-socket-mem="1024,0"
/usr/share/openvswitch/scripts/ovs-vsctl --no-wait set Open_vSwitch . other_config:dpdk-init=true
/usr/share/openvswitch/scripts/ovs-vsctl --no-wait set Open_vSwitch . other_config:pmd-cpu-mask=0x3
/usr/share/openvswitch/scripts/ovs-vsctl --no-wait set Open_vSwitch . other_config:dpdk-lcore-mask=0xc

# Start ovn-controller
/usr/share/openvswitch/scripts/ovn-ctl restart_controller
/usr/share/openvswitch/scripts/ovn-ctl restart_controller_vtep

# Set remote ovn-sb for ovn-controller to connect to
ovs-vsctl set open . external-ids:ovn-remote=tcp:"${OVN_SB_SERVICE_HOST}":"${OVN_SB_SERVICE_PORT}"
ovs-vsctl set open . external-ids:ovn-remote-probe-interval=10000
ovs-vsctl set open . external-ids:ovn-encap-type=geneve

tail -f /var/log/openvswitch/ovs-vswitchd.log
