#!/bin/bash
set -euo pipefail

OVS_DB_ADDR=${1:-0.0.0.0}
OVS_DB_PORT=${1:-6640}
DB_NB_ADDR=${1:-0.0.0.0}
DB_NB_PORT=${1:-6641}
DB_SB_ADDR=${1:-0.0.0.0}
DB_SB_PORT=${1:-6642}

# Start ovs-db
/usr/share/openvswitch/scripts/ovs-ctl start --no-ovs-vswitchd --system-id=random
ovs-vsctl -t 1 set-manager ptcp:${OVS_DB_PORT}:${OVS_DB_ADDR} || true

# Start ovn-northd, ovn-nb and ovn-sb
/usr/share/openvswitch/scripts/ovn-ctl start_northd

# ovn-nb and ovn-sb listen on tcp ports for ovn-controller to connect
ovn-nbctl set-connection ptcp:${DB_NB_PORT}:${DB_NB_ADDR}
ovn-sbctl set-connection ptcp:${DB_SB_PORT}:${DB_SB_ADDR}

function quit {
    /usr/share/openvswitch/scripts/ovn-ctl stop_northd
    /usr/share/openvswitch/scripts/ovs-ctl stop
    exit 0
}
trap quit SIGTERM

tail -f /var/log/openvswitch/ovn-northd.log