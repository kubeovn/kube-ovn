#!/bin/bash
set -euo pipefail

function quit {
	ovs-ctl stop
	ovn-ctl stop_controller
	exit 0
}
trap quit EXIT

CONFIG_FILE=/opt/ovs-config/config.cfg

OVN_REMOTE_PROBE_INTERVAL=${OVN_REMOTE_PROBE_INTERVAL:-10000}
OVN_REMOTE_OPENFLOW_INTERVAL=${OVN_REMOTE_OPENFLOW_INTERVAL:-180}

echo "OVN_REMOTE_PROBE_INTERVAL is set to $OVN_REMOTE_PROBE_INTERVAL"
echo "OVN_REMOTE_OPENFLOW_INTERVAL is set to $OVN_REMOTE_OPENFLOW_INTERVAL"

# Check if config file exists, create default one if not
if ! test -f "$CONFIG_FILE"; then
	mkdir -p $(dirname ${CONFIG_FILE})
	printf %s\\n {dpdk-socket-mem=\"1024\",dpdk-init=true,dpdk-hugepage-dir=/dev/hugepages} > $CONFIG_FILE
fi

# Start ovsdb
ovs-ctl restart --no-ovs-vswitchd --system-id=random

# Restrict the number of pthreads ovs-vswitchd creates to reduce the
# amount of RSS it uses on hosts with many cores
# https://bugzilla.redhat.com/show_bug.cgi?id=1571379
# https://bugzilla.redhat.com/show_bug.cgi?id=1572797
if [[ `nproc` -gt 12 ]]; then
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-revalidator-threads=4
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-handler-threads=10
fi

# Read the config and setup OVS
while IFS= read -r config_line
do
	if [[ $config_line ]] && [[ $config_line != \#* ]] ; then
		ovs-vsctl --no-wait set Open_vSwitch . other_config:$config_line
	fi
done < "$CONFIG_FILE"

# Start vswitchd
ovs-ctl restart --no-ovsdb-server --system-id=random
ovs-ctl --protocol=udp --dport=6081 enable-protocol

# Start ovn-controller
ovn-ctl restart_controller

# Set remote ovn-sb for ovn-controller to connect to
ovs-vsctl set open . external-ids:ovn-remote=tcp:"${OVN_SB_SERVICE_HOST}":"${OVN_SB_SERVICE_PORT}"
ovs-vsctl set open . external-ids:ovn-remote-probe-interval="${OVN_REMOTE_PROBE_INTERVAL}"
ovs-vsctl set open . external-ids:ovn-openflow-probe-interval="${OVN_REMOTE_OPENFLOW_INTERVAL}"
ovs-vsctl set open . external-ids:ovn-encap-type=geneve

tail --follow=name --retry /var/log/openvswitch/ovs-vswitchd.log
