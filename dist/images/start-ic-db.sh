#!/bin/bash
set -eo pipefail

LOCAL_IP=${LOCAL_IP:-$POD_IP}
ENABLE_BIND_LOCAL_IP=${ENABLE_BIND_LOCAL_IP:-true}
ENABLE_OVN_LEADER_CHECK=${ENABLE_OVN_LEADER_CHECK:-true}
readonly UUID_RE='[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}'

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

function ovndb_query_cluster_id {
    local db=""
    local port=""
    case $1 in
    ic_nb) db=OVN_IC_Northbound; port=6645 ;;
    ic_sb) db=OVN_IC_Southbound; port=6646 ;;
    *) echo "invalid database: $1"; return 2 ;;
    esac
    local query='["_Server",{"table":"Database","where":[["name","==","'$db'"]],"columns":["cid"],"op":"select"}]'
    if [[ "$ENABLE_SSL" == "false" ]]; then
        timeout 10 ovsdb-client query "$(gen_conn_addr "$2" "$port")" "$query"
    else
        timeout 10 ovsdb-client -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert query "$(gen_conn_addr "$2" "$port")" "$query"
    fi
}

function get_live_cluster_id {
    local db_type="$1"
    local require_quorum="${2:-false}"
    local live_cid=""
    local total_nodes=0
    local live_nodes=0
    local node_ip
    for node_ip in ${NODE_IPS//,/ }; do
        ((total_nodes += 1))
        [[ "$node_ip" == "$POD_IP" ]] && continue
        local cid=""
        cid=$(ovndb_query_cluster_id "$db_type" "$node_ip" 2>/dev/null | grep -oEm1 "$UUID_RE" || true)
        [[ "$cid" =~ ^${UUID_RE}$ ]] || continue
        ((live_nodes += 1))
        cid="${cid,,}"
        if [[ -n "$live_cid" && "$live_cid" != "$cid" ]]; then
            echo "conflicting live cluster IDs $live_cid and $cid for $db_type" >&2
            return 2
        fi
        live_cid="$cid"
    done
    [[ -n "$live_cid" ]] || return 1
    if [[ "$require_quorum" == "true" ]]; then
        local quorum=$((total_nodes / 2 + 1))
        if ((live_nodes < quorum)); then
            echo "only $live_nodes of $quorum required $db_type peers are reachable" >&2
            return 1
        fi
    fi
    echo "$live_cid"
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

function random_str {
    echo $RANDOM | md5sum | head -c 6
}

function archive_recovery_file {
    local source_file="$1"
    local backup_base="$2"
    local suffix="$3"
    local description="$4"
    if [[ ! -e "$source_file" ]]; then
        return 0
    fi

    local backup_file="$backup_base.$suffix-$(date +%s)-$(random_str)"
    echo "backup $description $source_file to $backup_file"
    mv "$source_file" "$backup_file"
}

function ovn_db_pre_start() {
    local db=""
    local port=""
    case $1 in
    ic_nb)
        db=OVN_IC_Northbound
        port=6645
        ;;
    ic_sb)
        db=OVN_IC_Southbound
        port=6646
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
        if grep -Fxq "$db_file has not joined the cluster, omitting" <<< "$message"; then
            return 0
        fi
        if [[ $status -ne 0 ]]; then
            echo "$message" >&2
            echo "detected invalid clustered database $db_file; rebuild a clean member from the live cluster"

            local repaired_db="$db_file.repaired-$(date +%s)-$(random_str)"
            cp "$db_file" "$repaired_db" || {
                echo "failed to copy $db_file before recovery; keeping the member unavailable" >&2
                return 1
            }
            if ! ovsdb-tool fix-cluster "$repaired_db" || ! ovsdb-tool check-cluster "$repaired_db"; then
                echo "failed to validate repaired database $repaired_db; keeping the original member file" >&2
                archive_recovery_file "$repaired_db" "$db_file" failed-fix "recovery copy" || return 1
                return 1
            fi

            local server_id=""
            server_id=$(ovsdb-tool db-sid "$repaired_db" || true)
            if ! echo -n "$server_id" | grep -qE '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$' || [[ "$server_id" == 00000000-0000-0000-0000-000000000000 ]]; then
                echo "failed to read server ID from repaired database $repaired_db" >&2
                archive_recovery_file "$repaired_db" "$db_file" invalid "recovery copy" || return 1
                return 1
            fi

            local live_cid=""
            live_cid=$(get_live_cluster_id "$1" true) || {
                echo "no consistent live $1 cluster is reachable; keeping the member unavailable" >&2
                archive_recovery_file "$repaired_db" "$db_file" pending-rejoin "recovery copy" || return 1
                return 1
            }

            local local_addr="$(gen_conn_addr "$POD_IP" "$port")"
            local remote_addr=()
            local ip
            for ip in ${NODE_IPS//,/ }; do
                if [[ "$POD_IP" != "$ip" ]]; then
                    remote_addr+=("$(gen_conn_addr "$ip" "$port")")
                fi
            done
            local db_new="$db_file.rejoin-$(date +%s)-$(random_str)"
            if ! ovsdb-tool --cid "$live_cid" --sid "$server_id" join-cluster "$db_new" "$db" "$local_addr" "${remote_addr[@]}"; then
                echo "failed to create a clean $db member; keeping the member unavailable" >&2
                archive_recovery_file "$repaired_db" "$db_file" pending-rejoin "recovery copy" || return 1
                archive_recovery_file "$db_new" "$db_file" failed-rejoin "new database file" || return 1
                return 1
            fi

            local db_backup="$db_file.backup-$(date +%s)-$(random_str)"
            if ! mv "$db_file" "$db_backup" || ! mv "$db_new" "$db_file"; then
                mv "$db_backup" "$db_file" 2>/dev/null || true
                echo "failed to install rebuilt database $db_file; keeping the member unavailable" >&2
                archive_recovery_file "$repaired_db" "$db_file" pending-rejoin "recovery copy" || return 1
                archive_recovery_file "$db_new" "$db_file" failed-rejoin "new database file" || return 1
                return 1
            fi
            rm -f "$repaired_db"
            echo "rebuilt $db with server ID $server_id; waiting for Raft synchronization"
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
