#!/bin/bash
set -eo pipefail

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

DB_NB_ADDR=${DB_NB_ADDR:-::}
DB_NB_PORT=${DB_NB_PORT:-6641}
DB_SB_ADDR=${DB_SB_ADDR:-::}
DB_SB_PORT=${DB_SB_PORT:-6642}
ENABLE_SSL=${ENABLE_SSL:-false}

. /usr/share/openvswitch/scripts/ovs-lib || exit 1

function random_str {
    echo $RANDOM | md5sum | head -c 6
}

function gen_conn_addr {
    if [[ "$ENABLE_SSL" == "false" ]]; then
        echo "tcp:[$1]:$2"
    else
        echo "ssl:[$1]:$2"
    fi
}

function gen_conn_str {
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
        x=$(for i in ${t}; do echo -n "tcp:[$i]:$1",; done| sed 's/,$//')
    else
        x=$(for i in ${t}; do echo -n "ssl:[$i]:$1",; done| sed 's/,$//')
    fi
    echo "$x"
}

function get_leader_ip {
    # Always use first node ip as leader, this option only take effect
    # when first bootstrap the cluster.
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    echo -n "${t}" | cut -f 1 -d " "
}

function quit {
    /usr/share/ovn/scripts/ovn-ctl stop_northd
    exit 0
}

function is_clustered {
  t=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
  if [[ "$ENABLE_SSL" == "false" ]]; then
    x=$(for i in ${t}; do echo -n "tcp:[${i}]:6641,"; done | sed 's/,/ /g')
    for i in ${x};
    do
      nb_leader=$(timeout 10 ovsdb-client query ${i} "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
      if [[ $nb_leader =~ "true" ]]
      then
        return 0
      fi
    done
  else
    x=$(for i in ${t}; do echo -n "ssl:[${i}]:6641,"; done| sed 's/,/ /g')
    for i in ${x};
    do
      nb_leader=$(timeout 10 ovsdb-client -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert query ${i} "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
      if [[ $nb_leader =~ "true" ]]
      then
        return 0
      fi
    done
  fi
  return 1
}

# create a new db file and join it to the cluster
# if the nb/sb db file is corrputed
function ovn_db_pre_start() {
    local db=""
    local port=""
    case $1 in
    nb)
        db=OVN_Northbound
        port=6643
        ;;
    sb)
        db=OVN_Southbound
        port=6644
        ;;
    *)
        echo "invalid database: $1"
        exit 1
        ;;
    esac

    local db_file="/etc/ovn/ovn${1}_db.db"
    [ ! -e "$db_file" ] && return
    ovsdb_tool check-cluster "$db_file" && return

    echo "detected database corruption for file $db_file, rebuild it."
    local sid=$(ovsdb-tool db-sid "$db_file")
    if ! echo -n "$sid" | grep -qE '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'; then
        echo "failed to get sid from $1 db file $db_file"
        return 1
    fi
    echo "get local server id $sid"

    local local_addr="$(gen_conn_addr $POD_IP $port)"
    echo "local address: $local_addr"

    local remote_addr=()
    local ips=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
    for ip in ${ips[*]}; do
        if [ ! "$POD_IP" = "$ip" ]; then
            remote_addr=(${remote_addr[*]} "$(gen_conn_addr $ip $port)")
        fi
    done
    echo "remote addresses: ${remote_addr[*]}"

    local db_new="$db_file.init-$(random_str)"
    echo "generating new database file $db_new"
    ovsdb_tool --sid $sid join-cluster "$db_new" $db $local_addr ${remote_addr[*]} || return 1

    local db_bak="$db_file.backup-$(random_str)"
    echo "backup $db_file to $db_bak"
    mv "$db_file" "$db_bak" || return 1

    echo "use new database file $db_new"
    mv "$db_new" "$db_file"
}

trap quit EXIT
if [[ "$ENABLE_SSL" == "false" ]]; then
    if [[ -z "$NODE_IPS" ]]; then
        /usr/share/ovn/scripts/ovn-ctl restart_northd
        ovn-nbctl --no-leader-only set-connection ptcp:"${DB_NB_PORT}":["${DB_NB_ADDR}"]
        ovn-nbctl --no-leader-only set Connection . inactivity_probe=180000
        ovn-nbctl --no-leader-only set NB_Global . options:use_logical_dp_groups=true

        ovn-sbctl --no-leader-only set-connection ptcp:"${DB_SB_PORT}":["${DB_SB_ADDR}"]
        ovn-sbctl --no-leader-only set Connection . inactivity_probe=180000
    else
        if [[ ! "$NODE_IPS" =~ "$POD_IP" ]]; then
            echo "ERROR! host ip $POD_IP not in env NODE_IPS $NODE_IPS"
            exit 1
        fi
        /usr/share/ovn/scripts/ovn-ctl stop_northd
        ovn_db_pre_start nb
        ovn_db_pre_start sb

        nb_leader_ip=$(get_leader_ip nb)
        sb_leader_ip=$(get_leader_ip sb)
        set +eo pipefail
        is_clustered
        result=$?
        set -eo pipefail
        # leader up only when no cluster and on first node
        if [[ ${result} -eq 1 &&  "$nb_leader_ip" == "${POD_IP}" ]]; then
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl \
                --db-nb-create-insecure-remote=yes \
                --db-sb-create-insecure-remote=yes \
                --db-nb-cluster-local-addr="[${POD_IP}]" \
                --db-sb-cluster-local-addr="[${POD_IP}]" \
                --db-nb-addr=[::] \
                --db-sb-addr=[::] \
                --ovn-northd-nb-db="$(gen_conn_str 6641)" \
                --ovn-northd-sb-db="$(gen_conn_str 6642)" \
                start_northd
            ovn-nbctl --no-leader-only set-connection ptcp:"${DB_NB_PORT}":[::]
            ovn-nbctl --no-leader-only set Connection . inactivity_probe=180000
            ovn-nbctl --no-leader-only set NB_Global . options:use_logical_dp_groups=true

            ovn-sbctl --no-leader-only set-connection ptcp:"${DB_SB_PORT}":[::]
            ovn-sbctl --no-leader-only set Connection . inactivity_probe=180000
        else
            # known leader always first
            set +eo pipefail
            if [ ${result} -eq 0 ]; then
                t=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
                for i in ${t};
                do
                    nb_leader=$(timeout 10 ovsdb-client query "tcp:${i}:6641" "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
                    if [[ $nb_leader =~ "true" ]]
                    then
                        nb_leader_ip=${i}
                        break
                    fi
                done
                for i in ${t};
                do
                    nb_leader=$(timeout 10 ovsdb-client query "tcp:${i}:6642" "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Southbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
                    if [[ $nb_leader =~ "true" ]]
                    then
                        sb_leader_ip=${i}
                        break
                    fi
                done
            fi
            set -eo pipefail
            # otherwise go to first node
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl \
                --db-nb-create-insecure-remote=yes \
                --db-sb-create-insecure-remote=yes \
                --db-nb-cluster-local-addr="[${POD_IP}]" \
                --db-sb-cluster-local-addr="[${POD_IP}]" \
                --db-nb-cluster-remote-addr="[${nb_leader_ip}]" \
                --db-sb-cluster-remote-addr="[${sb_leader_ip}]" \
                --db-nb-addr=[::] \
                --db-sb-addr=[::] \
                --ovn-northd-nb-db="$(gen_conn_str 6641)" \
                --ovn-northd-sb-db="$(gen_conn_str 6642)" \
                start_northd
        fi
    fi
else
    if [[ -z "$NODE_IPS" ]]; then
        /usr/share/ovn/scripts/ovn-ctl \
            --ovn-nb-db-ssl-key=/var/run/tls/key \
            --ovn-nb-db-ssl-cert=/var/run/tls/cert \
            --ovn-nb-db-ssl-ca-cert=/var/run/tls/cacert \
            --ovn-sb-db-ssl-key=/var/run/tls/key \
            --ovn-sb-db-ssl-cert=/var/run/tls/cert \
            --ovn-sb-db-ssl-ca-cert=/var/run/tls/cacert \
            --ovn-northd-ssl-key=/var/run/tls/key \
            --ovn-northd-ssl-cert=/var/run/tls/cert \
            --ovn-northd-ssl-ca-cert=/var/run/tls/cacert \
            restart_northd
        ovn-nbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set-connection pssl:"${DB_NB_PORT}":["${DB_NB_ADDR}"]
        ovn-nbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set Connection . inactivity_probe=180000
        ovn-nbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set NB_Global . options:use_logical_dp_groups=true

        ovn-sbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set-connection pssl:"${DB_SB_PORT}":["${DB_SB_ADDR}"]
        ovn-sbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set Connection . inactivity_probe=180000
    else
        if [[ ! "$NODE_IPS" =~ "$POD_IP" ]]; then
            echo "ERROR! host ip $POD_IP not in env NODE_IPS $NODE_IPS"
            exit 1
        fi
        /usr/share/ovn/scripts/ovn-ctl stop_northd
        ovn_db_pre_start nb
        ovn_db_pre_start sb

        nb_leader_ip=$(get_leader_ip nb)
        sb_leader_ip=$(get_leader_ip sb)
        set +eo pipefail
        is_clustered
        result=$?
        set -eo pipefail
        if [[ ${result} -eq 1  &&  "$nb_leader_ip" == "${POD_IP}" ]]; then
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl \
                --ovn-nb-db-ssl-key=/var/run/tls/key \
                --ovn-nb-db-ssl-cert=/var/run/tls/cert \
                --ovn-nb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-sb-db-ssl-key=/var/run/tls/key \
                --ovn-sb-db-ssl-cert=/var/run/tls/cert \
                --ovn-sb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-northd-ssl-key=/var/run/tls/key \
                --ovn-northd-ssl-cert=/var/run/tls/cert \
                --ovn-northd-ssl-ca-cert=/var/run/tls/cacert \
                --db-nb-cluster-local-addr="[${POD_IP}]" \
                --db-sb-cluster-local-addr="[${POD_IP}]" \
                --db-nb-addr=[::] \
                --db-sb-addr=[::] \
                --ovn-northd-nb-db="$(gen_conn_str 6641)" \
                --ovn-northd-sb-db="$(gen_conn_str 6642)" \
                start_northd
            ovn-nbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set-connection pssl:"${DB_NB_PORT}":[::]
            ovn-nbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set Connection . inactivity_probe=180000
            ovn-nbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set NB_Global . options:use_logical_dp_groups=true

            ovn-sbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set-connection pssl:"${DB_SB_PORT}":[::]
            ovn-sbctl --no-leader-only -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert set Connection . inactivity_probe=180000
        else
            # get leader if cluster exists
            set +eo pipefail
            if [[ ${result} -eq 0 ]]; then
                t=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
                for i in ${t};
                do
                    nb_leader=$(timeout 10 ovsdb-client -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert query "ssl:[${i}]:6641" "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
                    if [[ $nb_leader =~ "true" ]]
                    then
                      nb_leader_ip=${i}
                      break
                    fi
                done
                for i in ${t};
                do
                    nb_leader=$(timeout 10 ovsdb-client -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert query "ssl:[${i}]:6642" "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_Southbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
                    if [[ $nb_leader =~ "true" ]]
                    then
                      sb_leader_ip=${i}
                      break
                    fi
                done
            fi
            set -eo pipefail
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl \
                --ovn-nb-db-ssl-key=/var/run/tls/key \
                --ovn-nb-db-ssl-cert=/var/run/tls/cert \
                --ovn-nb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-sb-db-ssl-key=/var/run/tls/key \
                --ovn-sb-db-ssl-cert=/var/run/tls/cert \
                --ovn-sb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-northd-ssl-key=/var/run/tls/key \
                --ovn-northd-ssl-cert=/var/run/tls/cert \
                --ovn-northd-ssl-ca-cert=/var/run/tls/cacert \
                --db-nb-cluster-local-addr="[${POD_IP}]" \
                --db-sb-cluster-local-addr="[${POD_IP}]" \
                --db-nb-cluster-remote-addr="[${nb_leader_ip}]" \
                --db-sb-cluster-remote-addr="[${sb_leader_ip}]" \
                --db-nb-addr=[::] \
                --db-sb-addr=[::] \
                --ovn-northd-nb-db="$(gen_conn_str 6641)" \
                --ovn-northd-sb-db="$(gen_conn_str 6642)" \
                start_northd
        fi
    fi
fi

# Reclaim heap memory after compaction
# https://www.mail-archive.com/ovs-dev@openvswitch.org/msg48853.html
ovs-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/memory-trim-on-compaction on
ovs-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/memory-trim-on-compaction on

chmod 600 /etc/ovn/*
/kube-ovn/kube-ovn-leader-checker

