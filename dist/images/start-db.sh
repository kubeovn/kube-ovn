#!/bin/bash
set -euo pipefail

DB_NB_ADDR=${1:-0.0.0.0}
DB_NB_PORT=${1:-6641}
DB_SB_ADDR=${1:-0.0.0.0}
DB_SB_PORT=${1:-6642}

function gen_conn_str {
  t=$(echo -n ${NODE_IPS} | sed 's/[[:space:]]//g' | sed 's/,/ /g')
  x=$(for i in $t; do echo -n "tcp:$i:$1",; done| sed 's/,$//')
  echo "$x"
}

function get_first_node_ip {
    t=$(echo -n ${NODE_IPS} | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    echo -n $t | cut -f 1 -d " "
}

function quit {
    /usr/share/openvswitch/scripts/ovn-ctl stop_northd
    exit 0
}
trap quit EXIT

if [ -z "$NODE_IPS" ]; then
    /usr/share/openvswitch/scripts/ovn-ctl restart_northd
else
    /usr/share/openvswitch/scripts/ovn-ctl stop_northd

    first_node_ip=$(get_first_node_ip)
    if [ "$first_node_ip" == "${POD_IP}" ]; then
        # Start ovn-northd, ovn-nb and ovn-sb
        /usr/share/openvswitch/scripts/ovn-ctl \
            --db-nb-create-insecure-remote=yes \
            --db-sb-create-insecure-remote=yes \
            --db-nb-cluster-local-addr=${POD_IP} \
            --db-sb-cluster-local-addr=${POD_IP} \
            --ovn-northd-nb-db=$(gen_conn_str 6641) \
            --ovn-northd-sb-db=$(gen_conn_str 6642) \
            start_northd
    else
        while ! nc -z ${first_node_ip} ${DB_NB_PORT} </dev/null;
        do
            echo "sleep 5 seconds, waiting for ovn-nb ${first_node_ip}:${DB_NB_PORT} ready "
            sleep 5;
        done
        # Start ovn-northd, ovn-nb and ovn-sb
        /usr/share/openvswitch/scripts/ovn-ctl \
            --db-nb-create-insecure-remote=yes \
            --db-sb-create-insecure-remote=yes \
            --db-nb-cluster-local-addr=${POD_IP} \
            --db-sb-cluster-local-addr=${POD_IP} \
            --db-nb-cluster-remote-addr=$first_node_ip \
            --db-sb-cluster-remote-addr=$first_node_ip \
            --ovn-northd-nb-db=$(gen_conn_str 6641) \
            --ovn-northd-sb-db=$(gen_conn_str 6642) \
            start_northd
    fi
fi

# ovn-nb and ovn-sb listen on tcp ports for ovn-controller to connect
ovn-nbctl set-connection ptcp:${DB_NB_PORT}:${DB_NB_ADDR}
ovn-sbctl set-connection ptcp:${DB_SB_PORT}:${DB_SB_ADDR}

tail -f /var/log/openvswitch/ovn-northd.log