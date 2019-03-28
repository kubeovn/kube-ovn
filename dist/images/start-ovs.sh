#!/bin/bash
set -euo pipefail

# wait for ovn-sb ready
function wait_ovn_sb {
    if [[ -z "${OVN_SB_SERVICE_HOST}}" ]]; then
        echo "env OVN_SB_SERVICE_HOST not exists"
        exit 1
    fi
    if [[ -z "${OVN_SB_SERVICE_PORT}}" ]]; then
        echo "env OVN_SB_SERVICE_PORT not exists"
        exit 1
    fi
    while ! nc -z ${OVN_SB_SERVICE_HOST} ${OVN_SB_SERVICE_PORT} </dev/null;
    do
        echo "sleep 10 seconds, waiting for ovn-sb ${OVN_SB_SERVICE_HOST}:${OVN_SB_SERVICE_PORT} ready "
        sleep 10;
    done
}
wait_ovn_sb

function quit {
	/usr/share/openvswitch/scripts/ovs-ctl stop
	/usr/share/openvswitch/scripts/ovn-ctl stop_controller
	/usr/share/openvswitch/scripts/ovn-ctl stop_controller_vtep
	exit 0
}
trap quit SIGTERM

# Start vswitchd
/usr/share/openvswitch/scripts/ovs-ctl restart --no-ovs-vswitchd --system-id=random
# Restrict the number of pthreads ovs-vswitchd creates to reduce the
# amount of RSS it uses on hosts with many cores
# https://bugzilla.redhat.com/show_bug.cgi?id=1571379
# https://bugzilla.redhat.com/show_bug.cgi?id=1572797
if [[ `nproc` -gt 12 ]]; then
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-revalidator-threads=4
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-handler-threads=10
fi

# Start ovsdb
/usr/share/openvswitch/scripts/ovs-ctl restart --no-ovsdb-server  --system-id=random
/usr/share/openvswitch/scripts/ovs-ctl --protocol=udp --dport=6081 enable-protocol

# Start ovn-controller
/usr/share/openvswitch/scripts/ovn-ctl restart_controller
/usr/share/openvswitch/scripts/ovn-ctl restart_controller_vtep

# Set remote ovn-sb for ovn-controller to connect to
ovs-vsctl set open . external-ids:ovn-remote=tcp:${OVN_SB_SERVICE_HOST}:${OVN_SB_SERVICE_PORT}
ovs-vsctl set open . external-ids:ovn-encap-type=geneve
ovs-vsctl set open . external-ids:ovn-encap-ip=${POD_IP}


tail -f /var/log/openvswitch/ovs-vswitchd.log
