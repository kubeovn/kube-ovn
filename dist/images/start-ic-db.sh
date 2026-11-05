#!/bin/bash
set -eo pipefail

LOCAL_IP=${LOCAL_IP:-$POD_IP}
ENABLE_BIND_LOCAL_IP=${ENABLE_BIND_LOCAL_IP:-true}
ENABLE_OVN_LEADER_CHECK=${ENABLE_OVN_LEADER_CHECK:-true}

DB_ADDR=::
if [[ $ENABLE_BIND_LOCAL_IP == "true" ]]; then
    DB_ADDR="$POD_IP"
fi

function get_leader_addr {
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    echo -n "${t}" | cut -f 1 -d " "
}

function quit {
    local status=$?
    /usr/share/ovn/scripts/ovn-ctl stop_ic_ovsdb || echo "failed to stop interconnection databases during cleanup" >&2
    exit "$status"
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
        timeout 10 ovsdb-client -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert query $(gen_conn_addr $i $port) "$query"
    fi
}

function format_ovsdb_addr {
    if [[ "$1" =~ ^[0-9.]+$ || "$1" == *:* ]]; then
        echo "[$1]"
    else
        echo "$1"
    fi
}

function gen_conn_str {
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
        x=$(for i in ${t}; do echo -n "tcp:$(format_ovsdb_addr "$i"):$1",; done| sed 's/,$//')
    else
        x=$(for i in ${t}; do echo -n "ssl:$(format_ovsdb_addr "$i"):$1",; done| sed 's/,$//')
    fi
    echo "$x"
}

function gen_conn_addr {
    if [[ "$ENABLE_SSL" == "false" ]]; then
        echo "tcp:$(format_ovsdb_addr "$1"):$2"
    else
        echo "ssl:$(format_ovsdb_addr "$1"):$2"
    fi
}

function ovn_db_pre_start() {
    local db=""
    case $1 in
    ic_nb)
        db=OVN_IC_Northbound
        ;;
    ic_sb)
        db=OVN_IC_Southbound
        ;;
    *)
        echo "invalid database: $1"
        exit 1
        ;;
    esac

    local db_file="/etc/ovn/ovn${1}_db.db"
    [[ ! -e "$db_file" ]] && return 0
    local actual_db_name
    if ! actual_db_name=$(ovsdb-tool db-name "$db_file"); then
        echo "ERROR! cannot read database identity from $db_file; preserve it for explicit recovery" >&2
        return 1
    fi
    if [[ "$actual_db_name" != "$db" ]]; then
        echo "ERROR! expected $db in $db_file, found $actual_db_name; refusing to replace the database" >&2
        return 1
    fi
    if ovsdb-tool db-is-clustered "$db_file"; then
        local message
        local status=0
        message=$(ovsdb-tool check-cluster "$db_file" 2>&1) || status=$?
        if [[ $status -ne 0 ]] && ! grep -Fxq "$db_file has not joined the cluster, omitting" <<< "$message"; then
            echo "$message" >&2
            # Rewriting a valid prefix or rejoining an arbitrary peer can
            # discard committed history. Keep the original file unchanged.
            echo "ERROR! refusing to automatically repair or rebuild $db_file; preserve the database and reconcile cluster history before explicit recovery" >&2
            return 1
        fi
    elif ! ovsdb-tool db-is-standalone "$db_file"; then
        echo "ERROR! cannot determine the storage model of $db_file; preserve it for explicit recovery" >&2
        return 1
    fi
    return 0
}


function is_clustered {
  t=$(echo -n "${NODE_IPS}" | sed 's/,/ /g')
  if [[ "$ENABLE_SSL" == "false" ]]; then
    x=$(for i in ${t}; do echo -n "tcp:[${i}]:6645,"; done | sed 's/,/ /g')
    for i in ${x};
    do
      nb_leader=$(timeout 10 ovsdb-client query ${i} "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_IC_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
      if [[ $nb_leader =~ "true" ]]
      then
        return 0
      fi
    done
  else
    x=$(for i in ${t}; do echo -n "ssl:[${i}]:6645,"; done| sed 's/,/ /g')
    for i in ${x};
    do
      nb_leader=$(timeout 10 ovsdb-client -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert query ${i} "[\"_Server\",{\"table\":\"Database\",\"where\":[[\"name\",\"==\", \"OVN_IC_Northbound\"]],\"columns\": [\"leader\"],\"op\":\"select\"}]")
      if [[ $nb_leader =~ "true" ]]
      then
        return 0
      fi
    done
  fi
  return 1
}

trap quit EXIT
/usr/share/ovn/scripts/ovn-ctl stop_ic_ovsdb
ovn_db_pre_start ic_nb
ovn_db_pre_start ic_sb

if [[ -z "$NODE_IPS" && -z "$LOCAL_IP" ]]; then
    /usr/share/ovn/scripts/ovn-ctl --db-ic-nb-create-insecure-remote=yes --db-ic-sb-create-insecure-remote=yes --db-ic-nb-addr="[::]" --db-ic-sb-addr="[::]" start_ic_ovsdb
    /usr/share/ovn/scripts/ovn-ctl status_ic_ovsdb
else
    ic_nb_leader_addr=$(get_leader_addr nb)
    ic_sb_leader_addr=$(get_leader_addr sb)
    set +eo pipefail
    is_clustered
    result=$?
    set -eo pipefail
    # leader up only when no cluster and on first node
    if [[ ${result} -eq 1 &&  "$ic_nb_leader_addr" == "${POD_IP}" ]]; then
        echo "leader start with local ${LOCAL_IP} and cluster $(gen_conn_str 6647)"
        /usr/share/ovn/scripts/ovn-ctl  --db-ic-nb-create-insecure-remote=yes \
        --db-ic-sb-create-insecure-remote=yes \
        --db-ic-sb-cluster-local-addr="[${LOCAL_IP}]" \
        --db-ic-nb-cluster-local-addr="[${LOCAL_IP}]" \
        --ovn-ic-nb-db="$(gen_conn_str 6647)" \
        --ovn-ic-sb-db="$(gen_conn_str 6648)" \
        --db-ic-nb-addr=[$DB_ADDR] \
        --db-ic-sb-addr=[$DB_ADDR] \
        start_ic_ovsdb
        /usr/share/ovn/scripts/ovn-ctl status_ic_ovsdb
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
                    ic_nb_leader_addr=${i}
                    break
                fi
            done
            for i in ${t};
            do
                sb_leader=$(ovndb_query_leader sb $i)
                if [[ $sb_leader =~ "true" ]]
                then
                    ic_sb_leader_addr=${i}
                    break
                fi
            done
        fi
        set -eo pipefail
        echo "follower start with local ${LOCAL_IP}, ovn-ic-nb leader ${ic_nb_leader_addr} ovn-ic-sb leader ${ic_sb_leader_addr}"
        /usr/share/ovn/scripts/ovn-ctl  --db-ic-nb-create-insecure-remote=yes \
        --db-ic-sb-create-insecure-remote=yes \
        --db-ic-sb-cluster-local-addr="[${LOCAL_IP}]" \
        --db-ic-nb-cluster-local-addr="[${LOCAL_IP}]" \
        --db-ic-nb-cluster-remote-addr="[${ic_nb_leader_addr}]" \
        --db-ic-sb-cluster-remote-addr="[${ic_sb_leader_addr}]" \
        --ovn-ic-nb-db="$(gen_conn_str 6647)" \
        --ovn-ic-sb-db="$(gen_conn_str 6648)" \
        --db-ic-nb-addr=[$DB_ADDR] \
        --db-ic-sb-addr=[$DB_ADDR] \
        start_ic_ovsdb
    fi
fi

if [[ $ENABLE_OVN_LEADER_CHECK == "true" ]]; then
    chmod 600 /etc/ovn/*
    /kube-ovn/kube-ovn-leader-checker \
        --probeInterval=${OVN_LEADER_PROBE_INTERVAL} \
        --remoteAddresses="${NODE_IPS}" \
        --isICDBServer=true
else
    # Compatible with controller deployment methods before kube-ovn 1.11.16
    TS_NAME=${TS_NAME:-ts}
    PROTOCOL=${PROTOCOL:-ipv4}
    if [ "$PROTOCOL" = "ipv4" ]; then
      TS_CIDR=${TS_CIDR:-169.254.100.0/24}
    elif [ "$PROTOCOL" = "ipv6" ]; then
      TS_CIDR=${TS_CIDR:-fe80:a9fe:64::/112}
    elif [ "$PROTOCOL" = "dual" ]; then
      TS_CIDR=${TS_CIDR:-"169.254.100.0/24,fe80:a9fe:64::/112"}
    fi
    ovn-ic-nbctl \
        --may-exist ts-add "$TS_NAME" -- \
        set Transit_Switch "$TS_NAME" external_ids:subnet="$TS_CIDR" external_ids:vendor=kube-ovn
    tail --follow=name --retry /var/log/ovn/ovsdb-server-ic-nb.log
fi
