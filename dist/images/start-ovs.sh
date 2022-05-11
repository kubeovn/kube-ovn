#!/bin/bash
set -euo pipefail

HW_OFFLOAD=${HW_OFFLOAD:-false}
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}
TUNNEL_TYPE=${TUNNEL_TYPE:-geneve}
FLOW_WAIT=${FLOW_WAIT:-5}

# Check required kernel module
modinfo openvswitch
modinfo geneve
modinfo ip_tables

# CentOS 8 might not load iptables module by default, which will hurt nat function
modprobe ip_tables

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

# When ovs-vswitchd starts with this value set as true, it will neither flush or
# expire previously set datapath flows nor will it send and receive any
# packets to or from the datapath. Please check ovs-vswitchd.conf.db.5.txt
ovs-vsctl --no-wait set open_vswitch . other_config:flow-restore-wait="true"

if [ "$HW_OFFLOAD" = "true" ]; then
  ovs-vsctl --no-wait set open_vswitch . other_config:hw-offload=true
else
  ovs-vsctl --no-wait set open_vswitch . other_config:hw-offload=false
fi

# Start vswitchd. restart will automatically set/unset flow-restore-wait which is not what we want
/usr/share/openvswitch/scripts/ovs-ctl start --no-ovsdb-server --system-id=random
/usr/share/openvswitch/scripts/ovs-ctl --protocol=udp --dport=6081 enable-protocol

sleep 1

function handle_underlay_bridges() {
    bridges=($(ovs-vsctl --no-heading --columns=name find bridge external-ids:vendor=kube-ovn))
    for br in ${bridges[@]}; do
        echo "handle bridge $br"
        ip link set $br up

        ports=($(ovs-vsctl list-ports $br))
        for port in ${ports[@]}; do
            port_type=$(ovs-vsctl --no-heading --columns=type find interface name=$port)
            if [ ! "x$port_type" = 'x""' ]; then
              continue
            fi

            echo "handle port $port on bridge $br"
            ipv4_routes=($(ip -4 route show dev $port | tr ' ' '#'))
            ipv6_routes=($(ip -6 route show dev $port | tr ' ' '#'))

            set +o pipefail
            addresses=($(ip addr show dev $port | grep -E '^\s*inet[6]?\s+' | grep -w global | awk '{print $2}'))
            set -o pipefail

            # transfer IP addresses
            for addr in ${addresses[@]}; do
                printf "delete address $addr on $port\n"
                ip addr del $addr dev $port || true
                printf "add/replace address $addr to $br\n"
                ip addr replace $addr dev $br
            done

            # transfer IPv4 routes
            default_ipv4_routes=()
            for route in ${ipv4_routes[@]}; do
                r=$(echo $route | tr '#' ' ')
                if echo $r | grep -q -w 'scope link'; then
                    printf "add/replace IPv4 route $r to $br\n"
                    ip -4 route replace $r dev $br
                else
                    default_ipv4_routes=(${default_ipv4_routes[@]} $route)
                fi
            done
            for route in ${default_ipv4_routes[@]}; do
                r=$(echo $route | tr '#' ' ')
                printf "add/replace IPv4 route $r to $br\n"
                ip -4 route replace $r dev $br
            done

            # transfer IPv6 routes
            default_ipv6_routes=()
            for route in ${ipv6_routes[@]}; do
                r=$(echo $route | tr '#' ' ')
                if echo $r | grep -q -w 'scope link'; then
                    printf "add/replace IPv6 route $r to $br\n"
                    ip -6 route replace $r dev $br
                else
                    default_ipv6_routes=(${default_ipv6_routes[@]} $route)
                fi
            done
            for route in ${default_ipv6_routes[@]}; do
                r=$(echo $route | tr '#' ' ')
                printf "add/replace IPv6 route $r to $br\n"
                ip -6 route replace $r dev $br
            done
        done
    done
}

handle_underlay_bridges

function gen_conn_str {
  if [[ -z "${OVN_DB_IPS}" ]]; then
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x="tcp:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
    else
      x="ssl:[${OVN_SB_SERVICE_HOST}]:${OVN_SB_SERVICE_PORT}"
    fi
  else
    t=$(echo -n "${OVN_DB_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x=$(for i in ${t}; do echo -n "tcp:[$i]:$1",; done| sed 's/,$//')
    else
      x=$(for i in ${t}; do echo -n "ssl:[$i]:$1",; done| sed 's/,$//')
    fi
  fi
  echo "$x"
}
# Set remote ovn-sb for ovn-controller to connect to
ovs-vsctl set open . external-ids:ovn-remote="$(gen_conn_str 6642)"
ovs-vsctl set open . external-ids:ovn-remote-probe-interval=10000
ovs-vsctl set open . external-ids:ovn-openflow-probe-interval=180
ovs-vsctl set open . external-ids:ovn-encap-type="${TUNNEL_TYPE}"
ovs-vsctl set open . external-ids:hostname="${KUBE_NODE_NAME}"

# Start ovn-controller
if [[ "$ENABLE_SSL" == "false" ]]; then
  /usr/share/ovn/scripts/ovn-ctl restart_controller
else
  /usr/share/ovn/scripts/ovn-ctl --ovn-controller-ssl-key=/var/run/tls/key --ovn-controller-ssl-cert=/var/run/tls/cert --ovn-controller-ssl-ca-cert=/var/run/tls/cacert restart_controller
fi

# Wait ovn-controller finish init flow compute and update it to vswitchd,
# then update flow-restore-wait to indicate vswitchd to process flows
sleep ${FLOW_WAIT}
ovs-vsctl --no-wait set open_vswitch . other_config:flow-restore-wait="false"

chmod 600 /etc/openvswitch/*
tail -f /var/log/ovn/ovn-controller.log
