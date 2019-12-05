#!/bin/bash
set -eo pipefail

DB_NB_ADDR=${DB_NB_ADDR:-::}
DB_NB_PORT=${DB_NB_PORT:-6641}
DB_SB_ADDR=${DB_SB_ADDR:-::}
DB_SB_PORT=${DB_SB_PORT:-6642}

function gen_conn_str {
  t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
  x=$(for i in ${t}; do echo -n "tcp:$i:$1",; done| sed 's/,$//')
  echo "$x"
}

function get_leader_ip {
  while true; do
    leader=$(kubectl get ep -n "${POD_NAMESPACE}" | grep ovn-"$1" | awk '{print $2}' | cut -d ":" -f1)
    if [ "$leader" == "<none>" ]; then
      break
    else
      if [[ "$leader" != "${POD_IP}" ]]; then
        echo "$leader"
        return
      else
        echo "leader cannot be self, waiting new leader"
        sleep 5
      fi
    fi
  done
    # If no available leader the first ip will be the leader
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    echo -n "${t}" | cut -f 1 -d " "
}

function quit {
    /usr/share/openvswitch/scripts/ovn-ctl stop_northd
    exit 0
}
trap quit EXIT

if [[ -z "$NODE_IPS" ]]; then
    /usr/share/openvswitch/scripts/ovn-ctl restart_northd
    ovn-nbctl set-connection ptcp:"${DB_NB_PORT}":["${DB_NB_ADDR}"]
    ovn-sbctl set-connection ptcp:"${DB_SB_PORT}":["${DB_SB_ADDR}"]
    ovn-sbctl set Connection . inactivity_probe=0
else
    /usr/share/openvswitch/scripts/ovn-ctl stop_northd

    nb_leader_ip=$(get_leader_ip nb)
    sb_leader_ip=$(get_leader_ip sb)
    if [[ "$nb_leader_ip" == "${POD_IP}" ]]; then
        # Start ovn-northd, ovn-nb and ovn-sb
        /usr/share/openvswitch/scripts/ovn-ctl \
            --db-nb-create-insecure-remote=yes \
            --db-sb-create-insecure-remote=yes \
            --db-nb-cluster-local-addr="${POD_IP}" \
            --db-sb-cluster-local-addr="${POD_IP}" \
            --ovn-northd-nb-db=$(gen_conn_str 6641) \
            --ovn-northd-sb-db=$(gen_conn_str 6642) \
            start_northd

        # ovn-nb and ovn-sb listen on tcp ports for ovn-controller to connect
        ovn-nbctl set-connection ptcp:"${DB_NB_PORT}":["${DB_NB_ADDR}"]
        ovn-sbctl set-connection ptcp:"${DB_SB_PORT}":["${DB_SB_ADDR}"]
        ovn-sbctl set Connection . inactivity_probe=0
    else
        while ! nc -z "${nb_leader_ip}" "${DB_NB_PORT}" >/dev/null;
        do
            echo "sleep 5 seconds, waiting for ovn-nb ${nb_leader_ip}:${DB_NB_PORT} ready "
            sleep 5;
        done
        while ! nc -z "${sb_leader_ip}" "${DB_SB_PORT}" >/dev/null;
        do
            echo "sleep 5 seconds, waiting for ovn-sb ${sb_leader_ip}:${DB_NB_PORT} ready "
            sleep 5;
        done

        # Start ovn-northd, ovn-nb and ovn-sb
        /usr/share/openvswitch/scripts/ovn-ctl \
            --db-nb-create-insecure-remote=yes \
            --db-sb-create-insecure-remote=yes \
            --db-nb-cluster-local-addr="${POD_IP}" \
            --db-sb-cluster-local-addr="${POD_IP}" \
            --db-nb-cluster-remote-addr="${nb_leader_ip}" \
            --db-sb-cluster-remote-addr="${sb_leader_ip}" \
            --ovn-northd-nb-db=$(gen_conn_str 6641) \
            --ovn-northd-sb-db=$(gen_conn_str 6642) \
            start_northd
    fi
fi

tail -f /var/log/openvswitch/ovn-northd.log
