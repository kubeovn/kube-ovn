#!/bin/bash
set -eo pipefail

DEBUG_WRAPPER=${DEBUG_WRAPPER:-}
ENABLE_COMPACT=${ENABLE_COMPACT:-false}
PROBE_INTERVAL=${PROBE_INTERVAL:-180000}
OVN_NORTHD_N_THREADS=${OVN_NORTHD_N_THREADS:-1}
OVN_NORTHD_PROBE_INTERVAL=${OVN_NORTHD_PROBE_INTERVAL:-5000}
OVN_VERSION_COMPATIBILITY=${OVN_VERSION_COMPATIBILITY:-}
DEBUG_OPT="--ovn-northd-wrapper=$DEBUG_WRAPPER --ovsdb-nb-wrapper=$DEBUG_WRAPPER --ovsdb-sb-wrapper=$DEBUG_WRAPPER"

echo "PROBE_INTERVAL is set to $PROBE_INTERVAL"
echo "OVN_LEADER_PROBE_INTERVAL is set to $OVN_LEADER_PROBE_INTERVAL"
echo "OVN_NORTHD_N_THREADS is set to $OVN_NORTHD_N_THREADS"
echo "ENABLE_COMPACT is set to $ENABLE_COMPACT"

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

DB_CLUSTER_ADDR=${DB_CLUSTER_ADDR:-${POD_IP}}
NB_PORT=${NB_PORT:-6641}
SB_PORT=${SB_PORT:-6642}
NB_CLUSTER_PORT=${NB_CLUSTER_PORT:-6643}
SB_CLUSTER_PORT=${SB_CLUSTER_PORT:-6644}
ENABLE_SSL=${ENABLE_SSL:-false}
ENABLE_BIND_LOCAL_IP=${ENABLE_BIND_LOCAL_IP:-false}

echo "ENABLE_SSL is set to $ENABLE_SSL"
echo "ENABLE_BIND_LOCAL_IP is set to $ENABLE_BIND_LOCAL_IP"

DB_ADDR=::
DB_ADDRESSES=::
if [[ $ENABLE_BIND_LOCAL_IP == "true" ]]; then
    DB_ADDR="$POD_IP"
    DB_ADDRESSES="$POD_IPS"
fi

SSL_OPTIONS=
if [ "$ENABLE_SSL" != "false" ]; then
    SSL_OPTIONS="-p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert"
fi

. /usr/share/openvswitch/scripts/ovs-lib || exit 1

function random_str {
    echo $RANDOM | md5sum | head -c 6
}

function gen_listen_addr {
    if [[ "$ENABLE_SSL" == "false" ]]; then
        echo "ptcp:$2:[$1]"
    else
        echo "pssl:$2:[$1]"
    fi
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

function ovndb_query_leader {
    local db=""
    local db_eval=""
    case $1 in
    nb)
        db=OVN_Northbound
        db_eval="NB"
        ;;
    sb)
        db=OVN_Southbound
        db_eval="SB"
        ;;
    *)
        echo "invalid database: $1"
        exit 1
        ;;
    esac

    eval port="\$${db_eval}_PORT"
    query='["_Server",{"table":"Database","where":[["name","==","'$db'"]],"columns":["leader"],"op":"select"}]'
    if [[ "$ENABLE_SSL" == "false" ]]; then 
        timeout 10 ovsdb-client query $(gen_conn_addr $i $port) "$query"
    else
        timeout 10 ovsdb-client $SSL_OPTIONS query $(gen_conn_addr $i $port) "$query"
    fi
}

function quit {
    /usr/share/ovn/scripts/ovn-ctl stop_northd
    exit 0
}

function is_clustered {
    for i in $(echo -n "${NODE_IPS}" | sed 's/,/ /g'); do 
      nb_leader=$(ovndb_query_leader nb $i)
      if [[ $nb_leader =~ "true" ]]; then
        return 0
      fi
    done
  return 1
}

function set_nb_version_compatibility() {
    if [ -n "$OVN_VERSION_COMPATIBILITY" ]; then
        if ! ovn-nbctl --db=$(gen_conn_str 6641) $SSL_OPTIONS get NB_Global . options | grep -q version_compatibility=; then
            echo "setting ovn NB_Global option version_compatibility to ${OVN_VERSION_COMPATIBILITY}"
            ovn-nbctl --db=$(gen_conn_str 6641) $SSL_OPTIONS set NB_Global . options:version_compatibility=${OVN_VERSION_COMPATIBILITY}
            return
        fi
        value=`ovn-nbctl --db=$(gen_conn_str 6641) $SSL_OPTIONS get NB_Global . options:version_compatibility | sed -e 's/^"//' -e 's/"$//'`
        echo "ovn nb global option version_compatibility is set to $value"
        if [ "$value" != "_$OVN_VERSION_COMPATIBILITY" ]; then
            ovn-nbctl --db=$(gen_conn_str 6641) $SSL_OPTIONS set NB_Global . options:version_compatibility=${OVN_VERSION_COMPATIBILITY}
        fi
    fi
}

# create a new db file and join it to the cluster
# if the nb/sb db file is corrputed
function ovn_db_pre_start() {
    local db=""
    local db_eval=""
    case $1 in
    nb)
        db=OVN_Northbound
        db_eval=NB
        ;;
    sb)
        db=OVN_Southbound
        db_eval=SB
        ;;
    *)
        echo "invalid database: $1"
        exit 1
        ;;
    esac

    local db_file="/etc/ovn/ovn${1}_db.db"
    if [ -e "$db_file" ]; then
        if ovsdb-tool db-is-clustered "$db_file"; then
            local msg=$(ovsdb-tool check-cluster "$db_file" 2>&1) || true
            if echo $msg | grep -q 'has not joined the cluster'; then
                local birth_time=$(stat --format=%W $db_file)
                local now=$(date +%s)
                if [ $(($now - $birth_time)) -ge 120 ]; then
                    echo "ovn db file $db_file exists for more than 120s, remove it."
                    rm -f "$db_file" || return 1
                fi
                return
            fi

            if ! ovsdb-tool check-cluster "$db_file"; then
                local db_bak="$db_file.backup-$(date +%s)-$(random_str)"
                echo "backup $db_file to $db_bak"
                cp "$db_file" "$db_bak" || return 1

                echo "detected database corruption for file $db_file, try to fix it."
                local fixed=0
                if ovsdb-tool fix-cluster "$db_file"; then
                    echo "checking whether database file $db_file has been fixed."
                    if ovsdb-tool check-cluster "$db_file"; then
                        fixed=1
                    fi
                fi
                if [ $fixed -ne 1 ]; then
                    echo "failed to fix database file $db_file, rebuild it."
                    local sid=$(ovsdb-tool db-sid "$db_file")
                    if ! echo -n "$sid" | grep -qE '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'; then
                        echo "failed to get sid from db file $db_file"
                        return 1
                    fi
                    echo "get local server id $sid"

                    eval port="\$${db_eval}_CLUSTER_PORT"
                    local local_addr="$(gen_conn_addr $DB_CLUSTER_ADDR $port)"
                    echo "local address: $local_addr"

                    local remote_addr=()
                    local node_ips=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
                    for node_ip in ${node_ips[*]}; do
                        if [ ! "$node_ip" = "$DB_CLUSTER_ADDR" ]; then
                            remote_addr=(${remote_addr[*]} "$(gen_conn_addr $node_ip $port)")
                        fi
                    done
                    echo "remote addresses: ${remote_addr[*]}"

                    local db_new="$db_file.init-$(date +%s)-$(random_str)"
                    echo "generating new database file $db_new"
                    if [ ${#remote_addr[*]} -ne 0 ]; then
                        ovsdb-tool --sid $sid join-cluster "$db_new" $db $local_addr ${remote_addr[*]} || return 1

                        echo "use new database file $db_new"
                        mv "$db_new" "$db_file"
                    fi
                fi
            fi
        fi
    fi

    # create local config
    local config_db="/etc/ovn/ovn${1}_local_config.db"
    test -e $config_db && rm -f $config_db
    ovsdb-tool create $config_db /usr/share/openvswitch/local-config.ovsschema
    eval port="\$${db_eval}_PORT"
    local i=0
    for ip in ${DB_ADDRESSES//,/ }; do
        addr="$(gen_listen_addr $ip $port)"
        if [ $i -eq 0 ]; then
            ovsdb-tool transact $config_db '[
                "Local_Config",
                {"op": "insert", "table": "Config", "row": {"connections": ["named-uuid", "nameduuid"]}},
                {"op": "insert", "table": "Connection", "uuid-name": "nameduuid", "row": {"target": "'$addr'"}}
            ]'
        else
            ovsdb-tool transact $config_db '[
                "Local_Config",
                {"op": "insert", "table": "Connection", "uuid-name": "nameduuid", "row": {"target": "'$addr'"}},
                {"op": "mutate", "table": "Config", "where": [], "mutations": [["connections", "insert", ["set", [["named-uuid", "nameduuid"]]]]]}
            ]'
        fi
        i=$((i+1))
    done
}

trap quit EXIT
if [[ "$ENABLE_SSL" == "false" ]]; then
    if [[ -z "$NODE_IPS" ]]; then
        /usr/share/ovn/scripts/ovn-ctl restart_northd
        ovn-nbctl --no-leader-only set-connection ptcp:"${NB_PORT}":["${DB_ADDR}"]
        ovn-nbctl --no-leader-only set Connection . inactivity_probe=${PROBE_INTERVAL}
        ovn-nbctl --no-leader-only set NB_Global . options:northd_probe_interval=${OVN_NORTHD_PROBE_INTERVAL}
        ovn-nbctl --no-leader-only set NB_Global . options:use_logical_dp_groups=true

        ovn-sbctl --no-leader-only set-connection ptcp:"${SB_PORT}":["${DB_ADDR}"]
        ovn-sbctl --no-leader-only set Connection . inactivity_probe=${PROBE_INTERVAL}
    else
        if ! echo "$NODE_IPS" | tr ',' '\n' | grep '^'`echo "$DB_CLUSTER_ADDR" | sed 's/\./\\\./g'`'$'; then
            echo "ERROR! host ip $DB_CLUSTER_ADDR not in env NODE_IPS $NODE_IPS"
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
        # leader up only when no cluster and on the first/only node
        if [[ ${result} -eq 1 && "$nb_leader_ip" == "$DB_CLUSTER_ADDR" ]]; then
            ovn_ctl_args="$DEBUG_OPT \
                --db-nb-create-insecure-remote=yes \
                --db-sb-create-insecure-remote=yes \
                --db-nb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-sb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-nb-cluster-local-port=$NB_CLUSTER_PORT \
                --db-sb-cluster-local-port=$SB_CLUSTER_PORT \
                --db-nb-addr=[$DB_ADDR] \
                --db-sb-addr=[$DB_ADDR] \
                --db-nb-port=$NB_PORT \
                --db-sb-port=$SB_PORT \
                --db-nb-use-remote-in-db=no \
                --db-sb-use-remote-in-db=no \
                --ovn-northd-nb-db=$(gen_conn_str 6641) \
                --ovn-northd-sb-db=$(gen_conn_str 6642) "
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                start_nb_ovsdb -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnnb_local_config.db
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                start_sb_ovsdb -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnsb_local_config.db
            set_nb_version_compatibility
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                --ovn-manage-ovsdb=no --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS}" start_northd
            ovn-nbctl --no-leader-only set NB_Global . options:inactivity_probe=${PROBE_INTERVAL}
            ovn-sbctl --no-leader-only set SB_Global . options:inactivity_probe=${PROBE_INTERVAL}
            ovn-nbctl --no-leader-only set NB_Global . options:northd_probe_interval=${OVN_NORTHD_PROBE_INTERVAL}
            ovn-nbctl --no-leader-only set NB_Global . options:use_logical_dp_groups=true
        else
            # known leader always first
            set +eo pipefail
            if [ ${result} -eq 0 ]; then
                t=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
                for i in ${t};
                do
                    nb_leader=$(ovndb_query_leader nb $i)
                    if [[ $nb_leader =~ "true" ]]
                    then
                        nb_leader_ip=${i}
                        break
                    fi
                done
                for i in ${t};
                do
                    sb_leader=$(ovndb_query_leader sb $i)
                    if [[ $sb_leader =~ "true" ]]
                    then
                        sb_leader_ip=${i}
                        break
                    fi
                done
            fi
            set -eo pipefail
            # otherwise go to first node
            ovn_ctl_args="$DEBUG_OPT \
                --db-nb-create-insecure-remote=yes \
                --db-sb-create-insecure-remote=yes \
                --db-nb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-sb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-nb-cluster-remote-addr=[$nb_leader_ip] \
                --db-sb-cluster-remote-addr=[$sb_leader_ip] \
                --db-nb-cluster-local-port=$NB_CLUSTER_PORT \
                --db-sb-cluster-local-port=$SB_CLUSTER_PORT \
                --db-nb-cluster-remote-port=$NB_CLUSTER_PORT \
                --db-sb-cluster-remote-port=$SB_CLUSTER_PORT \
                --db-nb-addr=[$DB_ADDR] \
                --db-sb-addr=[$DB_ADDR] \
                --db-nb-port=$NB_PORT \
                --db-sb-port=$SB_PORT \
                --db-nb-use-remote-in-db=no \
                --db-sb-use-remote-in-db=no \
                --ovn-northd-nb-db=$(gen_conn_str 6641) \
                --ovn-northd-sb-db=$(gen_conn_str 6642)"
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl \
                $ovn_ctl_args \
                start_nb_ovsdb \
                -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnnb_local_config.db
            /usr/share/ovn/scripts/ovn-ctl \
                $ovn_ctl_args \
                start_sb_ovsdb \
                -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnsb_local_config.db
            set_nb_version_compatibility
            /usr/share/ovn/scripts/ovn-ctl \
                $ovn_ctl_args \
                --ovn-manage-ovsdb=no \
                --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS}" \
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
            --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS}" \
            restart_northd
        ovn-nbctl --no-leader-only $SSL_OPTIONS set-connection pssl:"${NB_PORT}":["${DB_ADDR}"]
        ovn-nbctl --no-leader-only $SSL_OPTIONS set Connection . inactivity_probe=${PROBE_INTERVAL}
        ovn-nbctl --no-leader-only $SSL_OPTIONS set NB_Global . options:use_logical_dp_groups=true

        ovn-sbctl --no-leader-only $SSL_OPTIONS set-connection pssl:"${SB_PORT}":["${DB_ADDR}"]
        ovn-sbctl --no-leader-only $SSL_OPTIONS set Connection . inactivity_probe=${PROBE_INTERVAL}
    else
        if ! echo "$NODE_IPS" | tr ',' '\n' | grep '^'`echo "$DB_CLUSTER_ADDR" | sed 's/\./\\\./g'`'$'; then
            echo "ERROR! host ip $DB_CLUSTER_ADDR not in env NODE_IPS $NODE_IPS"
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
        if [[ ${result} -eq 1  &&  "$nb_leader_ip" == "${DB_CLUSTER_ADDR}" ]]; then
            ovn_ctl_args="$DEBUG_OPT
                --ovn-nb-db-ssl-key=/var/run/tls/key \
                --ovn-nb-db-ssl-cert=/var/run/tls/cert \
                --ovn-nb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-sb-db-ssl-key=/var/run/tls/key \
                --ovn-sb-db-ssl-cert=/var/run/tls/cert \
                --ovn-sb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-northd-ssl-key=/var/run/tls/key \
                --ovn-northd-ssl-cert=/var/run/tls/cert \
                --ovn-northd-ssl-ca-cert=/var/run/tls/cacert \
                --db-nb-cluster-local-proto=ssl \
                --db-sb-cluster-local-proto=ssl \
                --db-nb-cluster-remote-proto=ssl \
                --db-sb-cluster-remote-proto=ssl \
                --db-nb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-sb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-nb-addr=[$DB_ADDR] \
                --db-sb-addr=[$DB_ADDR] \
                --db-nb-port=$NB_PORT \
                --db-sb-port=$SB_PORT \
                --db-nb-use-remote-in-db=no \
                --db-sb-use-remote-in-db=no \
                --ovn-northd-nb-db=$(gen_conn_str 6641) \
                --ovn-northd-sb-db=$(gen_conn_str 6642)"
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                start_nb_ovsdb -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnnb_local_config.db
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                start_sb_ovsdb -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnsb_local_config.db
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                --ovn-manage-ovsdb=no --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS}" start_northd
            ovn-nbctl --no-leader-only $SSL_OPTIONS set NB_Global . options:northd_probe_interval=${OVN_NORTHD_PROBE_INTERVAL}
            ovn-nbctl --no-leader-only $SSL_OPTIONS set NB_Global . options:use_logical_dp_groups=true
        else
            # get leader if cluster exists
            set +eo pipefail
            if [[ ${result} -eq 0 ]]; then
                t=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
                for i in ${t};
                do
                    nb_leader=$(ovndb_query_leader nb $i)
                    if [[ $nb_leader =~ "true" ]]
                    then
                      nb_leader_ip=${i}
                      break
                    fi
                done
                for i in ${t};
                do
                    sb_leader=$(ovndb_query_leader sb $i)
                    if [[ $sb_leader =~ "true" ]]
                    then
                      sb_leader_ip=${i}
                      break
                    fi
                done
            fi
            set -eo pipefail
            ovn_ctl_args="$DEBUG_OPT
                --ovn-nb-db-ssl-key=/var/run/tls/key \
                --ovn-nb-db-ssl-cert=/var/run/tls/cert \
                --ovn-nb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-sb-db-ssl-key=/var/run/tls/key \
                --ovn-sb-db-ssl-cert=/var/run/tls/cert \
                --ovn-sb-db-ssl-ca-cert=/var/run/tls/cacert \
                --ovn-northd-ssl-key=/var/run/tls/key \
                --ovn-northd-ssl-cert=/var/run/tls/cert \
                --ovn-northd-ssl-ca-cert=/var/run/tls/cacert \
                --db-nb-cluster-local-proto=ssl \
                --db-sb-cluster-local-proto=ssl \
                --db-nb-cluster-remote-proto=ssl \
                --db-sb-cluster-remote-proto=ssl \
                --db-nb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-sb-cluster-local-addr=[$DB_CLUSTER_ADDR] \
                --db-nb-cluster-remote-addr=[$nb_leader_ip] \
                --db-sb-cluster-remote-addr=[$sb_leader_ip] \
                --db-nb-cluster-local-port=$NB_CLUSTER_PORT \
                --db-sb-cluster-local-port=$SB_CLUSTER_PORT \
                --db-nb-cluster-remote-port=$NB_CLUSTER_PORT \
                --db-sb-cluster-remote-port=$SB_CLUSTER_PORT \
                --db-nb-addr=[$DB_ADDR] \
                --db-sb-addr=[$DB_ADDR] \
                --db-nb-port=$NB_PORT \
                --db-sb-port=$SB_PORT \
                --db-nb-use-remote-in-db=no \
                --db-sb-use-remote-in-db=no \
                --ovn-northd-nb-db=$(gen_conn_str 6641) \
                --ovn-northd-sb-db=$(gen_conn_str 6642)"
            # Start ovn-northd, ovn-nb and ovn-sb
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                start_nb_ovsdb -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnnb_local_config.db
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                start_sb_ovsdb -- \
                --remote=db:Local_Config,Config,connections \
                /etc/ovn/ovnsb_local_config.db
            set_nb_version_compatibility
            /usr/share/ovn/scripts/ovn-ctl $ovn_ctl_args \
                --ovn-manage-ovsdb=no --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS}" start_northd
        fi
    fi
fi

# Reclaim heap memory after compaction
# https://www.mail-archive.com/ovs-dev@openvswitch.org/msg48853.html
ovn-appctl -t /var/run/ovn/ovnnb_db.ctl ovsdb-server/memory-trim-on-compaction on
ovn-appctl -t /var/run/ovn/ovnsb_db.ctl ovsdb-server/memory-trim-on-compaction on

chmod 600 /etc/ovn/*
/kube-ovn/kube-ovn-leader-checker \
    --probeInterval=${OVN_LEADER_PROBE_INTERVAL} \
    --enableCompact=${ENABLE_COMPACT} \
    --remoteAddresses="${NODE_IPS}"
