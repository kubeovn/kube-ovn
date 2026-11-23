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

function is_valid_uuid {
    local uuid="${1,,}"
    [[ "$uuid" =~ ^${UUID_RE}$ ]] && \
        [[ "$uuid" != "00000000-0000-0000-0000-000000000000" ]]
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

function rebuild_clustered_database_from_raft_header {
    local db_file="$1"
    local hdr_file="$2"
    local db_name="$3"
    local db_type="$4"
    local local_addr="$5"
    shift 5
    local remote_addr=("$@")

    if [[ ! -s "$hdr_file" || ${#remote_addr[@]} -eq 0 ]]; then
        echo "cannot rebuild $db_name from missing raft header or peer addresses" >&2
        return 1
    fi

    local header_cid=""
    local server_id=""
    header_cid=$(sed -nE 's/.*"cluster_id"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "$hdr_file" | head -n 1)
    server_id=$(sed -nE 's/.*"server_id"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "$hdr_file" | head -n 1)
    header_cid="${header_cid,,}"
    server_id="${server_id,,}"
    if ! is_valid_uuid "$header_cid" || ! is_valid_uuid "$server_id"; then
        echo "raft header $hdr_file has an invalid cluster or server ID" >&2
        return 1
    fi

    local live_cid=""
    live_cid=$(get_live_cluster_id "$db_type" true) || {
        echo "no consistent live $db_type quorum is reachable; keeping $db_file unchanged" >&2
        return 1
    }
    if [[ "$header_cid" != "$live_cid" ]]; then
        echo "raft header cluster ID $header_cid does not match live cluster ID $live_cid" >&2
        return 1
    fi

    local db_new
    db_new="$db_file.rejoin-$(date +%s)-$(random_str)"
    echo "rebuilding $db_name from raft header $hdr_file with server ID $server_id"
    if ! ovsdb-tool rejoin-cluster "$db_new" "$hdr_file" "$local_addr" "${remote_addr[@]}"; then
        echo "failed to create a clean $db_name member from raft header" >&2
        rm -f "$db_new"
        return 1
    fi
    if [[ ! -s "$db_new" ]]; then
        echo "ovsdb-tool rejoin-cluster created no database file for $db_name" >&2
        rm -f "$db_new"
        return 1
    fi

    local db_backup
    db_backup="$db_file.backup-$(date +%s)-$(random_str)"
    if ! mv "$db_file" "$db_backup"; then
        echo "failed to preserve original database $db_file; refusing to replace it" >&2
        rm -f "$db_new"
        return 1
    fi
    if ! mv "$db_new" "$db_file"; then
        mv "$db_backup" "$db_file" || true
        echo "failed to install rebuilt database $db_file; keeping the member unavailable" >&2
        rm -f "$db_new"
        return 1
    fi
    echo "rebuilt $db_name with server ID $server_id; waiting for Raft synchronization"
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

    local db_file="/etc/ovn/ovn_${1}_db.db"
    local hdr_file="/etc/ovn/ovn_${1}_db.hdr"
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
            local local_addr="$(gen_conn_addr "$POD_IP" "$port")"
            local remote_addr=()
            local ip
            for ip in ${NODE_IPS//,/ }; do
                if [[ "$POD_IP" != "$ip" ]]; then
                    remote_addr+=("$(gen_conn_addr "$ip" "$port")")
                fi
            done
            rebuild_clustered_database_from_raft_header "$db_file" "$hdr_file" "$db" "$1" "$local_addr" "${remote_addr[@]}" || return 1
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
