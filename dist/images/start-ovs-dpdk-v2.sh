#!/bin/bash

set -euo pipefail

DPDK_TUNNEL_IFACE=${DPDK_TUNNEL_IFACE:-br-phy}
TUNNEL_TYPE=${TUNNEL_TYPE:-geneve}

OVS_DPDK_CONFIG_FILE=/opt/ovs-config/ovs-dpdk-config
if ! test -f "$OVS_DPDK_CONFIG_FILE"; then
    echo "missing ovs dpdk config"
    exit 1
fi
source $OVS_DPDK_CONFIG_FILE

# link sock
mkdir -p /usr/local/var/run

if [ -L /usr/local/var/run/openvswitch ]
then
     echo "sock exist"
else
     echo "link sock"
     ln -s /var/run/openvswitch /usr/local/var/run/openvswitch
fi

export PATH=$PATH:/usr/share/openvswitch/scripts
export PATH=$PATH:/usr/share/ovn/scripts

function quit {
  gen_name=$(kubectl -n "${POD_NAMESPACE}" get pod "${POD_NAME}" -o jsonpath='{.metadata.generateName}')
  revision_hash=$(kubectl -n "${POD_NAMESPACE}" get pod "${POD_NAME}" -o jsonpath='{.metadata.labels.controller-revision-hash}')
  revision=$(kubectl -n "${POD_NAMESPACE}" get controllerrevision "${gen_name}${revision_hash}" -o jsonpath='{.revision}')
  ds_name=${gen_name%-}
  latest_revision=$(kubectl -n kube-system get controllerrevision --no-headers | awk '$2 == "daemonset.apps/'$ds_name'" {print $3}' | sort -nr | head -n1)
  if [ "x$latest_revision" = "x$revision" ]; then
    # stop ovn-controller/ovs only when the processes are in the same cgroup
    pid=$(/usr/share/ovn/scripts/ovn-ctl status_controller | awk '{print $NF}')
    if cgroup_match "${pid}" self; then
      /usr/share/ovn/scripts/grace_stop_ovn_controller
      /usr/share/openvswitch/scripts/ovs-ctl stop
    fi
  fi

  exit 0
}
trap quit EXIT

CONFIG_FILE=/opt/ovs-config/config.cfg

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



if ! ovs-vsctl br-exists ${DPDK_TUNNEL_IFACE}; then
ovs-vsctl --may-exist add-br ${DPDK_TUNNEL_IFACE} \
    -- set Bridge ${DPDK_TUNNEL_IFACE} datapath_type=netdev \
    -- br-set-external-id ${DPDK_TUNNEL_IFACE} bridge-id ${DPDK_TUNNEL_IFACE} \
    -- set bridge ${DPDK_TUNNEL_IFACE} fail-mode=standalone

OPTS=""
IPS=""
for ((i=0;i< ${#DPDK_DEV[@]};i++));
do
   echo ${DPDK_DEV[i]};
   S=" -- set Interface p$i type=dpdk options:dpdk-devargs=${DPDK_DEV[i]}"
   OPTS="$OPTS$S"
   IPS=$IPS" p"$i
done

case ${BOND_TYPE} in
  "")
    ovs-vsctl --timeout 10 add-port ${DPDK_TUNNEL_IFACE} dpdk0 ${OPTS};;
  "active-backup"|1)
    ovs-vsctl --timeout 10 add-bond ${DPDK_TUNNEL_IFACE} dpdk0 ${IPS} ${OPTS}
    ovs-vsctl set port dpdk0 bond_mode=active-backup;;
  "balance-slb"|0)
    ovs-vsctl --timeout 10 add-bond ${DPDK_TUNNEL_IFACE} dpdk0 ${IPS} ${OPTS}
    ovs-vsctl set port dpdk0 bond_mode=balance-slb;;
  "lacp"|4)
    ovs-vsctl --timeout 10 add-bond ${DPDK_TUNNEL_IFACE} dpdk0 ${IPS} ${OPTS}
    ovs-vsctl set port dpdk0 bond_mode=balance-tcp
    ovs-vsctl set port dpdk0 lacp=active;;
  *)
    echo "wrong ovs dpdk bond_type config"
    exit 1;;
esac

ovs-vsctl set port ${DPDK_TUNNEL_IFACE} tag=${VLAN_TAG}

fi

ip link set ${DPDK_TUNNEL_IFACE} up

ip addr replace ${ENCAP_IP} dev ${DPDK_TUNNEL_IFACE}

ovs-vsctl --may-exist add-br br-int \
  -- set Bridge br-int datapath_type=netdev \
  -- br-set-external-id br-int bridge-id br-int \
  -- set bridge br-int fail-mode=secure



# Start ovn-controller
ovn-ctl restart_controller

# Set remote ovn-sb for ovn-controller to connect to
ovs-vsctl set open . external-ids:ovn-remote=tcp:"${OVN_SB_SERVICE_HOST}":"${OVN_SB_SERVICE_PORT}"
ovs-vsctl set open . external-ids:ovn-remote-probe-interval=10000
ovs-vsctl set open . external-ids:ovn-openflow-probe-interval=180
ovs-vsctl set open . external-ids:ovn-encap-type="${TUNNEL_TYPE}"

tail --follow=name --retry /var/log/openvswitch/ovs-vswitchd.log
