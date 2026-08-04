#!/bin/bash
set -eo pipefail

DEBUG_WRAPPER=${DEBUG_WRAPPER:-}
ENABLE_COMPACT=${ENABLE_COMPACT:-false}
PROBE_INTERVAL=${PROBE_INTERVAL:-180000}
OVN_NORTHD_N_THREADS=${OVN_NORTHD_N_THREADS:-1}
OVN_NORTHD_PROBE_INTERVAL=${OVN_NORTHD_PROBE_INTERVAL:-5000}
OVN_VERSION_COMPATIBILITY=${OVN_VERSION_COMPATIBILITY:-}
readonly UUID_RE='[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}'
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
TLS_MIN_VERSION=${TLS_MIN_VERSION:-}
TLS_MAX_VERSION=${TLS_MAX_VERSION:-}
TLS_CIPHER_SUITES=${TLS_CIPHER_SUITES:-}

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

. /kube-ovn/ovn-db-ssl-options.sh
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

function format_ovsdb_addr {
    if [[ "$1" =~ ^[0-9.]+$ || "$1" == *:* ]]; then
        echo "[$1]"
    else
        echo "$1"
    fi
}

function expand_k8s_svc_addr {
    if [[ "$1" != *.svc ]]; then
        echo "$1"
        return
    fi

    local fqdn
    fqdn=$(getent hosts "$1" | awk -v addr="$1" '{ for (i = 2; i <= NF; i++) { if ($i ~ "^" addr "\\.") { print $i; exit } } }')
    if [[ -z "$fqdn" ]]; then
        echo "ERROR! failed to resolve Kubernetes service address $1 to a FQDN" >&2
        exit 1
    fi
    echo "$fqdn"
}

function normalize_raft_addrs {
    local old_db_cluster_addr="$DB_CLUSTER_ADDR"
    DB_CLUSTER_ADDR="$(expand_k8s_svc_addr "$DB_CLUSTER_ADDR")"
    POD_IP="$(expand_k8s_svc_addr "${POD_IP:-}")"
    if [[ -n "$old_db_cluster_addr" && "${POD_IP:-}" == "$old_db_cluster_addr" ]]; then
        POD_IP="$DB_CLUSTER_ADDR"
    fi

    local addrs=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    local normalized=()
    for addr in ${addrs}; do
        normalized=(${normalized[*]} "$(expand_k8s_svc_addr "$addr")")
    done
    NODE_IPS="$(IFS=,; echo "${normalized[*]}")"
}

function gen_conn_addr {
    if [[ "$ENABLE_SSL" == "false" ]]; then
        echo "tcp:$(format_ovsdb_addr "$1"):$2"
    else
        echo "ssl:$(format_ovsdb_addr "$1"):$2"
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

function get_leader_addr {
    # Always use first node ip as leader, this option only take effect
    # when first bootstrap the cluster.
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    echo -n "${t}" | cut -f 1 -d " "
}

function ovndb_query_database {
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
    local query
    case "$3" in
    leader)
        query='["_Server",{"table":"Database","where":[["name","==","'$db'"]],"columns":["leader"],"op":"select"}]'
        ;;
    cid)
        query='["_Server",{"table":"Database","where":[["name","==","'$db'"]],"columns":["cid"],"op":"select"}]'
        ;;
    *)
        echo "invalid database query field: $3"
        return 2
        ;;
    esac

    if [[ "$ENABLE_SSL" == "false" ]]; then
        timeout 10 ovsdb-client query "$(gen_conn_addr "$2" "$port")" "$query"
    else
        timeout 10 ovsdb-client $SSL_OPTIONS query "$(gen_conn_addr "$2" "$port")" "$query"
    fi
}

function ovndb_query_leader {
    ovndb_query_database "$1" "$2" leader
}

function ovndb_query_cluster_id {
    local result
    result=$(ovndb_query_database "$1" "$2" cid) || return 1
    grep -oEm1 "$UUID_RE" <<< "$result"
}

function is_valid_uuid {
    local uuid="${1,,}"
    [[ "$uuid" =~ ^${UUID_RE}$ ]] && \
        [[ "$uuid" != "00000000-0000-0000-0000-000000000000" ]]
}

function get_live_cluster_id {
    local db_type="$1"
    local live_cid=""
    local node_ip
    for node_ip in ${NODE_IPS//,/ }; do
        if [[ "$node_ip" == "$DB_CLUSTER_ADDR" ]]; then
            continue
        fi

        local cid=""
        cid=$(ovndb_query_cluster_id "$db_type" "$node_ip" 2>/dev/null || true)
        if ! is_valid_uuid "$cid"; then
            continue
        fi
        cid="${cid,,}"
        if [[ -n "$live_cid" && "$live_cid" != "$cid" ]]; then
            echo "conflicting live cluster IDs $live_cid and $cid for $db_type" >&2
            return 2
        fi
        live_cid="$cid"
    done

    if [[ -z "$live_cid" ]]; then
        return 1
    fi
    echo "$live_cid"
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
            ovn-nbctl --db=$(gen_conn_str 6641) $SSL_OPTIONS set NB_Global . options:version_compatibility="${OVN_VERSION_COMPATIBILITY}"
            return
        fi
        value=$(ovn-nbctl --db=$(gen_conn_str 6641) $SSL_OPTIONS get NB_Global . options:version_compatibility | sed -e 's/^"//' -e 's/"$//')
        echo "ovn nb global option version_compatibility is set to $value"
        if [ "$value" != "_$OVN_VERSION_COMPATIBILITY" ]; then
            ovn-nbctl --db=$(gen_conn_str 6641) $SSL_OPTIONS set NB_Global . options:version_compatibility=${OVN_VERSION_COMPATIBILITY}
        fi
    fi
}

if [[ -n "${NODE_IPS:-}" ]]; then
    normalize_raft_addrs
fi

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

function rejoin_db_from_raft_header() {
    local db_file="$1"
    local hdr_file="$2"
    local db_name="$3"
    local db_type="$4"
    local local_addr="$5"
    shift 5
    local remote_addr=("$@")

    if [[ ${#remote_addr[@]} -eq 0 ]]; then
        echo "cannot rejoin cluster from raft header file $hdr_file without a remote address"
        archive_recovery_file "$hdr_file" "$hdr_file" invalid "unusable raft header file" || return 2
        return 1
    fi

    local header_cid=""
    local server_id=""
    header_cid=$(sed -nE 's/.*"cluster_id"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "$hdr_file" | head -n 1)
    server_id=$(sed -nE 's/.*"server_id"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "$hdr_file" | head -n 1)
    header_cid="${header_cid,,}"
    server_id="${server_id,,}"
    if ! is_valid_uuid "$server_id"; then
        echo "raft header file $hdr_file has a missing or zero server ID"
        archive_recovery_file "$hdr_file" "$hdr_file" invalid "unusable raft header file" || return 2
        return 1
    fi

    local live_cid=""
    local lookup_status=0
    local max_lookup_attempts=7
    local attempt
    for ((attempt = 1; attempt <= max_lookup_attempts; attempt++)); do
        lookup_status=0
        live_cid=$(get_live_cluster_id "$db_type") || lookup_status=$?
        if [[ $lookup_status -ne 1 || $attempt -eq $max_lookup_attempts ]]; then
            break
        fi
        echo "no reachable $db_type cluster found, retrying live cluster ID lookup ($attempt/$max_lookup_attempts)" >&2
        sleep 10
    done
    if [[ $lookup_status -eq 2 ]]; then
        return 2
    fi

    if is_valid_uuid "$header_cid" && { [[ $lookup_status -eq 1 ]] || [[ "$header_cid" == "$live_cid" ]]; }; then
        echo "generating new db file $db_file from raft header file $hdr_file"
        if ovsdb-tool rejoin-cluster "$db_file" "$hdr_file" "$local_addr" "${remote_addr[@]}"; then
            return 0
        fi
        echo "failed to generate db file $db_file from raft header file $hdr_file"
        archive_recovery_file "$db_file" "$db_file" failed-rejoin "database file left by failed rejoin" || return 2
    else
        echo "raft header cluster ID ${header_cid:-<missing>} does not match live cluster ID $live_cid"
    fi

    if [[ $lookup_status -eq 1 ]]; then
        echo "no reachable $db_name cluster found; continue with clean bootstrap"
        archive_recovery_file "$hdr_file" "$hdr_file" invalid "unusable raft header file" || return 2
        return 1
    fi

    local db_rejoin="$db_file.rejoin-$(date +%s)-$(random_str)"
    echo "rejoining $db_name cluster $live_cid with server ID $server_id"
    if ! ovsdb-tool --cid "$live_cid" --sid "$server_id" join-cluster "$db_rejoin" "$db_name" "$local_addr" "${remote_addr[@]}"; then
        echo "failed to rejoin $db_name cluster $live_cid with server ID $server_id"
        archive_recovery_file "$db_rejoin" "$db_file" failed-rejoin "database file left by failed join" || return 2
        return 2
    fi

    echo "use database file $db_rejoin"
    mv "$db_rejoin" "$db_file" || return 2
    archive_recovery_file "$hdr_file" "$hdr_file" invalid "unusable raft header file" || return 2
    return 0
}

function recover_clustered_database {
    local db_file="$1"
    local hdr_file="$2"
    local db_name="$3"
    local db_type="$4"
    local local_addr="$5"
    shift 5
    local remote_addr=("$@")

    local message
    message=$(ovsdb-tool check-cluster "$db_file" 2>&1) || true
    if grep -q 'has not joined the cluster' <<< "$message"; then
        local birth_time
        birth_time=$(stat --format=%W "$db_file")
        local now
        now=$(date +%s)
        if ((now - birth_time >= 120)); then
            echo "ovn db file $db_file exists for more than 120s, archive it."
            archive_recovery_file "$db_file" "$db_file" failed-join "database file" || return 1
            archive_recovery_file "$hdr_file" "$hdr_file" invalid "stale raft header file" || return 1
        fi
        return 0
    fi
    if ovsdb-tool check-cluster "$db_file"; then
        return 0
    fi

    local db_backup="$db_file.backup-$(date +%s)-$(random_str)"
    echo "backup $db_file to $db_backup"
    cp "$db_file" "$db_backup" || return 1
    echo "detected database corruption for file $db_file, try to fix it."
    if ovsdb-tool fix-cluster "$db_file" && ovsdb-tool check-cluster "$db_file"; then
        return 0
    fi

    local server_id=""
    server_id=$(ovsdb-tool db-sid "$db_file" || true)
    if ! is_valid_uuid "$server_id"; then
        echo "failed to get server ID from db file $db_file"
        return 1
    fi
    local live_cid=""
    live_cid=$(get_live_cluster_id "$db_type") || {
        echo "failed to get live cluster ID for $db_name"
        return 1
    }

    local db_new="$db_file.init-$(date +%s)-$(random_str)"
    echo "rebuilding $db_name database with cluster ID $live_cid and server ID $server_id"
    if ! ovsdb-tool --cid "$live_cid" --sid "$server_id" join-cluster "$db_new" "$db_name" "$local_addr" "${remote_addr[@]}"; then
        archive_recovery_file "$db_new" "$db_file" failed-rejoin "database file left by failed join" || return 1
        return 1
    fi
    mv "$db_new" "$db_file" || return 1
    archive_recovery_file "$hdr_file" "$hdr_file" invalid "stale raft header file" || return 1
}

function create_local_config {
    local db_type="$1"
    local db_eval="$2"
    local config_db="/etc/ovn/ovn${db_type}_local_config.db"
    test -e "$config_db" && rm -f "$config_db"
    ovsdb-tool create "$config_db" /usr/share/openvswitch/local-config.ovsschema
    eval port="\$${db_eval}_PORT"

    local index=0
    local ip
    for ip in ${DB_ADDRESSES//,/ }; do
        local addr
        addr="$(gen_listen_addr "$ip" "$port")"
        if [[ $index -eq 0 ]]; then
            ovsdb-tool transact "$config_db" '[
                "Local_Config",
                {"op": "insert", "table": "Config", "row": {"connections": ["named-uuid", "nameduuid"]}},
                {"op": "insert", "table": "Connection", "uuid-name": "nameduuid", "row": {"target": "'$addr'"}}
            ]'
        else
            ovsdb-tool transact "$config_db" '[
                "Local_Config",
                {"op": "insert", "table": "Connection", "uuid-name": "nameduuid", "row": {"target": "'$addr'"}},
                {"op": "mutate", "table": "Config", "where": [], "mutations": [["connections", "insert", ["set", [["named-uuid", "nameduuid"]]]]]}
            ]'
        fi
        index=$((index + 1))
    done
}

# create a new db file and join it to the cluster
# if the nb/sb db file is corrupted
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

    eval port="\$${db_eval}_CLUSTER_PORT"
    local local_addr
    local_addr="$(gen_conn_addr "$DB_CLUSTER_ADDR" "$port")"
    echo "local address: $local_addr"

    local remote_addr=()
    local node_ip
    for node_ip in ${NODE_IPS//,/ }; do
        if [[ "$node_ip" != "$DB_CLUSTER_ADDR" ]]; then
            remote_addr+=("$(gen_conn_addr "$node_ip" "$port")")
        fi
    done
    echo "remote addresses: ${remote_addr[*]}"

    local db_file="/etc/ovn/ovn${1}_db.db"
    local hdr_file="/etc/ovn/ovn${1}_db.hdr"
    if [[ -e "$db_file" ]]; then
        local actual_db_name=""
        actual_db_name=$(ovsdb-tool db-name "$db_file" || true)
        if [[ "$actual_db_name" != "$db" ]]; then
            echo "ovn db file $db_file is corrupted, archive it."
            archive_recovery_file "$db_file" "$db_file" backup "database file" || return 1
        fi
    fi

    if [[ ! -e "$db_file" && -e "$hdr_file" ]]; then
        echo "db file $db_file is missing, while raft header file $hdr_file exists."
        local rejoin_status=0
        rejoin_db_from_raft_header "$db_file" "$hdr_file" "$db" "$1" "$local_addr" "${remote_addr[@]}" || rejoin_status=$?
        if [[ $rejoin_status -eq 0 ]]; then
            return 0
        elif [[ $rejoin_status -ne 1 ]]; then
            return 1
        fi
    fi

    if [[ -e "$db_file" ]] && ovsdb-tool db-is-clustered "$db_file"; then
        recover_clustered_database "$db_file" "$hdr_file" "$db" "$1" "$local_addr" "${remote_addr[@]}" || return 1
    fi
    create_local_config "$1" "$db_eval"
}

trap quit EXIT
if [[ "$ENABLE_SSL" == "false" ]]; then
    if [[ -z "$NODE_IPS" ]]; then
        # Standalone (single-replica) mode. The pod can drift between nodes when
        # backed by a PV, so clean up any leftover sockets/pids on the host before
        # starting ovsdb-server.
        rm -f /var/run/ovn/*.pid /var/run/ovn/*.ctl 2>/dev/null || true
        /usr/share/ovn/scripts/ovn-ctl --ovn-northd-n-threads="${OVN_NORTHD_N_THREADS}" restart_northd
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

        nb_leader_addr=$(get_leader_addr nb)
        sb_leader_addr=$(get_leader_addr sb)
        set +eo pipefail
        is_clustered
        result=$?
        set -eo pipefail
        # leader up only when no cluster and on the first/only node
        if [[ ${result} -eq 1 && "$nb_leader_addr" == "$DB_CLUSTER_ADDR" ]]; then
            ovn_ctl_args="$DEBUG_OPT \
                --db-nb-create-insecure-remote=yes \
                --db-sb-create-insecure-remote=yes \
                --db-nb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
                --db-sb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
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
                        nb_leader_addr=${i}
                        break
                    fi
                done
                for i in ${t};
                do
                    sb_leader=$(ovndb_query_leader sb $i)
                    if [[ $sb_leader =~ "true" ]]
                    then
                        sb_leader_addr=${i}
                        break
                    fi
                done
            fi
            set -eo pipefail
            # otherwise go to first node
            ovn_ctl_args="$DEBUG_OPT \
                --db-nb-create-insecure-remote=yes \
                --db-sb-create-insecure-remote=yes \
                --db-nb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
                --db-sb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
                --db-nb-cluster-remote-addr=$(format_ovsdb_addr "$nb_leader_addr") \
                --db-sb-cluster-remote-addr=$(format_ovsdb_addr "$sb_leader_addr") \
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
        # Standalone (single-replica) mode. Same rationale as the non-SSL branch:
        # clean up stale sockets/pids so a drifted pod can come up on a new node.
        rm -f /var/run/ovn/*.pid /var/run/ovn/*.ctl 2>/dev/null || true
        /usr/share/ovn/scripts/ovn-ctl $(ovn_db_ssl_args /var/run/tls) \
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

        nb_leader_addr=$(get_leader_addr nb)
        sb_leader_addr=$(get_leader_addr sb)
        set +eo pipefail
        is_clustered
        result=$?
        set -eo pipefail
        if [[ ${result} -eq 1  &&  "$nb_leader_addr" == "${DB_CLUSTER_ADDR}" ]]; then
            ovn_ctl_args="$DEBUG_OPT
                $(ovn_db_ssl_args /var/run/tls) \
                --db-nb-cluster-local-proto=ssl \
                --db-sb-cluster-local-proto=ssl \
                --db-nb-cluster-remote-proto=ssl \
                --db-sb-cluster-remote-proto=ssl \
                --db-nb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
                --db-sb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
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
                      nb_leader_addr=${i}
                      break
                    fi
                done
                for i in ${t};
                do
                    sb_leader=$(ovndb_query_leader sb $i)
                    if [[ $sb_leader =~ "true" ]]
                    then
                      sb_leader_addr=${i}
                      break
                    fi
                done
            fi
            set -eo pipefail
            ovn_ctl_args="$DEBUG_OPT
                $(ovn_db_ssl_args /var/run/tls) \
                --db-nb-cluster-local-proto=ssl \
                --db-sb-cluster-local-proto=ssl \
                --db-nb-cluster-remote-proto=ssl \
                --db-sb-cluster-remote-proto=ssl \
                --db-nb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
                --db-sb-cluster-local-addr=$(format_ovsdb_addr "$DB_CLUSTER_ADDR") \
                --db-nb-cluster-remote-addr=$(format_ovsdb_addr "$nb_leader_addr") \
                --db-sb-cluster-remote-addr=$(format_ovsdb_addr "$sb_leader_addr") \
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

if [[ "$ENABLE_SSL" == "true" ]]; then
    bash /kube-ovn/kube-ovn-tls-reload.sh ovn-central &
fi

chmod 600 /etc/ovn/*
/kube-ovn/kube-ovn-leader-checker \
    --probeInterval=${OVN_LEADER_PROBE_INTERVAL} \
    --enableCompact=${ENABLE_COMPACT} \
    --remoteAddresses="${NODE_IPS}"
