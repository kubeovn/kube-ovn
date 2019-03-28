#!/bin/bash
set -euo pipefail

DB_NB_ADDR=${1:-0.0.0.0}
DB_NB_PORT=${1:-6641}
DB_SB_ADDR=${1:-0.0.0.0}
DB_SB_PORT=${1:-6642}

function quit {
    /usr/share/openvswitch/scripts/ovn-ctl stop_ovsdb
    /usr/share/openvswitch/scripts/ovn-ctl stop_northd
    exit 0
}
trap quit EXIT

# Start ovn-northd, ovn-nb and ovn-sb
/usr/share/openvswitch/scripts/ovn-ctl restart_northd

# ovn-nb and ovn-sb listen on tcp ports for ovn-controller to connect
ovn-nbctl set-connection ptcp:${DB_NB_PORT}:${DB_NB_ADDR}
ovn-sbctl set-connection ptcp:${DB_SB_PORT}:${DB_SB_ADDR}

tail -f /var/log/openvswitch/ovn-northd.log