#!/bin/bash
set -eo pipefail

LOCAL_IP=${LOCAL_IP:-$POD_IP}
ENABLE_BIND_LOCAL_IP=${ENABLE_BIND_LOCAL_IP:-true}
ENABLE_OVN_LEADER_CHECK=${ENABLE_OVN_LEADER_CHECK:-true}

DB_ADDR=::
if [[ $ENABLE_BIND_LOCAL_IP == "true" ]]; then
    DB_ADDR="$POD_IP"
fi

function get_leader_ip {
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    echo -n "${t}" | cut -f 1 -d " "
}

function quit {
    /usr/share/ovn/scripts/ovn-ctl stop_ic_ovsdb
    exit 0
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

function gen_conn_str {
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
        x=$(for i in ${t}; do echo -n "tcp:[$i]:$1",; done| sed 's/,$//')
    else
        x=$(for i in ${t}; do echo -n "ssl:[$i]:$1",; done| sed 's/,$//')
    fi
    echo "$x"
}

function gen_conn_addr {
    if [[ "$ENABLE_SSL" == "false" ]]; then
        echo "tcp:[$1]:$2"
    else
        echo "ssl:[$1]:$2"
    fi
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
    [ ! -e "$db_file" ] && return
    ! ovsdb-tool db-is-clustered "$db_file" && return
    ovsdb-tool check-cluster "$db_file" && return

    local db_bak="$db_file.backup-$(date +%s)-$(random_str)"
    echo "backup $db_file to $db_bak"
    cp "$db_file" "$db_bak" || return 1

    echo "detected database corruption for file $db_file, try to fix it."
    if ovsdb-tool fix-cluster "$db_file"; then
        echo "checking whether database file $db_file has been fixed."
        ovsdb-tool check-cluster "$db_file" && return
    fi

    echo "failed to fix database file $db_file, rebuild it."
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
    ovsdb-tool --sid $sid join-cluster "$db_new" $db $local_addr ${remote_addr[*]} || return 1

    echo "use new database file $db_new"
    mv "$db_new" "$db_file"
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
    ic_nb_leader_ip=$(get_leader_ip nb)
    ic_sb_leader_ip=$(get_leader_ip sb)
    set +eo pipefail
    is_clustered
    result=$?
    set -eo pipefail
    # leader up only when no cluster and on first node
    if [[ ${result} -eq 1 &&  "$ic_nb_leader_ip" == "${POD_IP}" ]]; then
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
        echo "follower start with local ${LOCAL_IP}, ovn-ic-nb leader ${ic_nb_leader_ip} ovn-ic-sb leader ${ic_sb_leader_ip}"
        /usr/share/ovn/scripts/ovn-ctl  --db-ic-nb-create-insecure-remote=yes \
        --db-ic-sb-create-insecure-remote=yes \
        --db-ic-sb-cluster-local-addr="[${LOCAL_IP}]" \
        --db-ic-nb-cluster-local-addr="[${LOCAL_IP}]" \
        --db-ic-nb-cluster-remote-addr="[${ic_nb_leader_ip}]" \
        --db-ic-sb-cluster-remote-addr="[${ic_sb_leader_ip}]" \
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
