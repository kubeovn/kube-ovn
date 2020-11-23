#!/bin/bash
set -euo pipefail

HW_OFFLOAD=${HW_OFFLOAD:-false}
ENABLE_SSL=${ENABLE_SSL:-false}

# https://bugs.launchpad.net/neutron/+bug/1776778
if grep -q "3.10.0-862" /proc/version
then
    echo "kernel version 3.10.0-862 has a nat related bug that will affect ovs function, please update to a version greater than 3.10.0-898"
    exit 1
fi

# https://bugs.launchpad.net/ubuntu/+source/linux/+bug/1794232
if [ ! -f "/proc/net/if_inet6" ] && grep -q "3.10" /proc/version ; then
    echo "geneve requires ipv6, please add ipv6.disable=0 to kernel follow the instruction below:"
    echo "
vi /etc/default/grub
find GRUB_CMDLINE_LINUX=  and change ipv6.disable=1 to ipv6.disable=0
grub2-mkconfig -o /boot/grub2/grub.cfg
reboot
cat /proc/cmdline"
    exit 1
fi

# wait for ovn-sb ready
function wait_ovn_sb {
    if [[ -z "${OVN_SB_SERVICE_HOST}" ]]; then
        echo "env OVN_SB_SERVICE_HOST not exists"
        exit 1
    fi
    if [[ -z "${OVN_SB_SERVICE_PORT}" ]]; then
        echo "env OVN_SB_SERVICE_PORT not exists"
        exit 1
    fi
    while ! nc -z "${OVN_SB_SERVICE_HOST}" "${OVN_SB_SERVICE_PORT}" </dev/null;
    do
        echo "sleep 10 seconds, waiting for ovn-sb ${OVN_SB_SERVICE_HOST}:${OVN_SB_SERVICE_PORT} ready "
        sleep 10;
    done
}
wait_ovn_sb

function quit {
	/usr/share/ovn/scripts/grace_stop_ovn_controller
	/usr/share/openvswitch/scripts/ovs-ctl stop
	exit 0
}
trap quit EXIT

# Start ovsdb
/usr/share/openvswitch/scripts/ovs-ctl restart --no-ovs-vswitchd --system-id=random
# Restrict the number of pthreads ovs-vswitchd creates to reduce the
# amount of RSS it uses on hosts with many cores
# https://bugzilla.redhat.com/show_bug.cgi?id=1571379
# https://bugzilla.redhat.com/show_bug.cgi?id=1572797
if [[ `nproc` -gt 12 ]]; then
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-revalidator-threads=4
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-handler-threads=10
fi

if [ "$HW_OFFLOAD" = "true" ]; then
  ovs-vsctl --no-wait set open_vswitch . other_config:hw-offload=true
else
  ovs-vsctl --no-wait set open_vswitch . other_config:hw-offload=false
fi

# Start vswitchd
/usr/share/openvswitch/scripts/ovs-ctl restart --no-ovsdb-server  --system-id=random
/usr/share/openvswitch/scripts/ovs-ctl --protocol=udp --dport=6081 enable-protocol

# Set remote ovn-sb for ovn-controller to connect to
if [[ "$ENABLE_SSL" == "false" ]]; then
  ovs-vsctl set open . external-ids:ovn-remote=tcp:"[${OVN_SB_SERVICE_HOST}]":"${OVN_SB_SERVICE_PORT}"
else
  ovs-vsctl set open . external-ids:ovn-remote=ssl:"[${OVN_SB_SERVICE_HOST}]":"${OVN_SB_SERVICE_PORT}"
fi
ovs-vsctl set open . external-ids:ovn-remote-probe-interval=10000
ovs-vsctl set open . external-ids:ovn-openflow-probe-interval=180
ovs-vsctl set open . external-ids:ovn-encap-type=geneve
ovs-vsctl set open . external-ids:hostname="${KUBE_NODE_NAME}"

# Start ovn-controller
if [[ "$ENABLE_SSL" == "false" ]]; then
  /usr/share/ovn/scripts/ovn-ctl restart_controller
else
  /usr/share/ovn/scripts/ovn-ctl --ovn-controller-ssl-key=/var/run/tls/key --ovn-controller-ssl-cert=/var/run/tls/cert --ovn-controller-ssl-ca-cert=/var/run/tls/cacert restart_controller
fi
chmod 600 /etc/openvswitch/*
tail -f /var/log/ovn/ovn-controller.log
