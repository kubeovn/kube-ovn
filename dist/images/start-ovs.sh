#!/bin/bash
set -euo pipefail



HW_OFFLOAD=${HW_OFFLOAD:-false}
ENABLE_SSL=${ENABLE_SSL:-false}
OVN_DB_IPS=${OVN_DB_IPS:-}
TUNNEL_TYPE=${TUNNEL_TYPE:-geneve}
FLOW_LIMIT=${FLOW_LIMIT:-10}
DEBUG_WRAPPER=${DEBUG_WRAPPER:-}
OVN_REMOTE_PROBE_INTERVAL=${OVN_REMOTE_PROBE_INTERVAL:-10000}
OVN_REMOTE_OPENFLOW_INTERVAL=${OVN_REMOTE_OPENFLOW_INTERVAL:-180}

echo "OVN_REMOTE_PROBE_INTERVAL is set to $OVN_REMOTE_PROBE_INTERVAL"
echo "OVN_REMOTE_OPENFLOW_INTERVAL is set to $OVN_REMOTE_OPENFLOW_INTERVAL"

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

function cgroup_match {
  if [ $1 -le 0 ]; then
    return 1
  fi
  hash1=$(md5sum /proc/"$1"/cgroup | awk '{print $1}')
  hash2=$(md5sum /proc/"$2"/cgroup | awk '{print $1}')
  test -n "$hash1" -a "x$hash1" = "x$hash2"
}

function quit {
  set -x
  gen_name=$(kubectl -n "${POD_NAMESPACE}" get pod "${POD_NAME}" -o jsonpath='{.metadata.generateName}')
  revision_hash=$(kubectl -n "${POD_NAMESPACE}" get pod "${POD_NAME}" -o jsonpath='{.metadata.labels.controller-revision-hash}')
  revision=$(kubectl -n "${POD_NAMESPACE}" get controllerrevision "${gen_name}${revision_hash}" --ignore-not-found -o jsonpath='{.revision}')
  ds_name=${gen_name%-}
  latest_revision=$(kubectl -n "${POD_NAMESPACE}" get controllerrevision --no-headers | awk '$2 == "daemonset.apps/'$ds_name'" {print $3}' | sort -nr | head -n1)
  if [ "x$revision" = "x" -o "x$latest_revision" = "x$revision" ]; then
    # stop ovn-controller/ovs only when the processes are in the same cgroup
    pid=$((/usr/share/ovn/scripts/ovn-ctl status_controller || printf '\n0') | tail -n1 | awk '{print $NF}')
    if cgroup_match "${pid}" self; then
      /usr/share/ovn/scripts/grace_stop_ovn_controller
    fi
    pid=$((/usr/share/openvswitch/scripts/ovs-ctl status || printf '\novsdb-server 0') | grep ovsdb-server | tail -n1 | awk '{print $NF}')
    if cgroup_match "${pid}" self; then
      /usr/share/openvswitch/scripts/ovs-ctl --no-ovs-vswitchd stop
    fi
    pid=$((/usr/share/openvswitch/scripts/ovs-ctl status || printf '\novs-vswitchd 0') | grep ovs-vswitchd | tail -n1 | awk '{print $NF}')
    if cgroup_match "${pid}" self; then
      /usr/share/openvswitch/scripts/ovs-ctl --no-ovsdb-server stop
    fi
  fi

  exit 0
}
trap quit EXIT

# update links to point to the iptables binaries
iptables -V

# Start ovsdb
/usr/share/openvswitch/scripts/ovs-ctl restart --no-ovs-vswitchd --system-id=random --ovsdb-server-wrapper="${DEBUG_WRAPPER}"
# Restrict the number of pthreads ovs-vswitchd creates to reduce the
# amount of RSS it uses on hosts with many cores
# https://bugzilla.redhat.com/show_bug.cgi?id=1571379
# https://bugzilla.redhat.com/show_bug.cgi?id=1572797
if [[ $(nproc) -gt 12 ]]; then
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-revalidator-threads=4
    ovs-vsctl --no-wait set Open_vSwitch . other_config:n-handler-threads=10
fi

if [ "$HW_OFFLOAD" = "true" ]; then
  ovs-vsctl --no-wait set open_vswitch . other_config:hw-offload=true
else
  ovs-vsctl --no-wait set open_vswitch . other_config:hw-offload=false
fi

# avoid warnings caused by ovs-vsctl
ovsdb_server_ctl="/var/run/openvswitch/ovsdb-server.$(cat /var/run/openvswitch/ovsdb-server.pid).ctl"
ovs-appctl -t "$ovsdb_server_ctl" vlog/set jsonrpc:file:err
ovs-appctl -t "$ovsdb_server_ctl" vlog/set reconnect:file:err

function handle_underlay_bridges() {
  bridges=($(ovs-vsctl --no-heading --columns=name find bridge external-ids:vendor=kube-ovn))
  for br in "${bridges[@]}"; do
    if ! ip link show "$br" >/dev/null; then
      # the bridge does not exist, leave it to be handled by kube-ovn-cni
      echo "deleting ovs bridge $br"
      ovs-vsctl --no-wait del-br "$br"
    fi
  done

  bridges=($(ovs-vsctl --no-heading --columns=name find bridge external-ids:vendor=kube-ovn external-ids:exchange-link-name=true))
  for br in "${bridges[@]}"; do
    if [ -z $(ip link show $br type openvswitch 2>/dev/null || true) ]; then
      # the bridge does not exist, leave it to be handled by kube-ovn-cni
      echo "deleting ovs bridge $br"
      ovs-vsctl --no-wait del-br "$br"
    fi
  done
}

handle_underlay_bridges

# Start vswitchd. restart will automatically set/unset flow-restore-wait which is not what we want
/usr/share/openvswitch/scripts/ovs-ctl restart --no-ovsdb-server --system-id=random --no-mlockall --ovs-vswitchd-wrapper="$DEBUG_WRAPPER"
/usr/share/openvswitch/scripts/ovs-ctl --protocol=udp --dport=6081 enable-protocol

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
ovs-vsctl set open . external-ids:ovn-remote-probe-interval="${OVN_REMOTE_PROBE_INTERVAL}"
ovs-vsctl set open . external-ids:ovn-openflow-probe-interval="${OVN_REMOTE_OPENFLOW_INTERVAL}"
ovs-vsctl set open . external-ids:ovn-encap-type="${TUNNEL_TYPE}"
ovs-vsctl set open . external-ids:hostname="${NODE_NAME}"

# Start ovn-controller
if [[ "$ENABLE_SSL" == "false" ]]; then
  /usr/share/ovn/scripts/ovn-ctl --ovn-controller-wrapper="$DEBUG_WRAPPER" restart_controller
else
  /usr/share/ovn/scripts/ovn-ctl --ovn-controller-ssl-key=/var/run/tls/key --ovn-controller-ssl-cert=/var/run/tls/cert --ovn-controller-ssl-ca-cert=/var/run/tls/cacert --ovn-controller-wrapper="$DEBUG_WRAPPER" restart_controller
fi

chmod 600 /etc/openvswitch/*
tail --follow=name --retry /var/log/ovn/ovn-controller.log
