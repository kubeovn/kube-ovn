#!/bin/bash
set -euo pipefail

function quit {
	ovs-ctl stop
	ovn-ctl stop_controller
	exit 0
}
trap quit EXIT

# Start ovsdb and vswitchd
ovs-ctl --no-ovs-vswitchd start
ovs-vsctl --no-wait set Open_vSwitch . other_config:dpdk-socket-mem="1024"
ovs-vsctl --no-wait set Open_vSwitch . other_config:dpdk-init=true
ovs-vsctl --no-wait set Open_vSwitch . other_config:dpdk-hugepage-dir=/dev/hugepages
ovs-ctl --no-ovsdb-server start

# Start ovn-controller
ovn-ctl restart_controller

# Set remote ovn-sb for ovn-controller to connect to
ovs-vsctl set open . external-ids:ovn-remote=tcp:"${OVN_SB_SERVICE_HOST}":"${OVN_SB_SERVICE_PORT}"
ovs-vsctl set open . external-ids:ovn-remote-probe-interval=10000
ovs-vsctl set open . external-ids:ovn-openflow-probe-interval=180
ovs-vsctl set open . external-ids:ovn-encap-type=geneve

tail -f /var/log/openvswitch/ovs-vswitchd.log
