#!/bin/bash
set -eo pipefail

TS_NAME=${TS_NAME:-ts}
PROTOCOL=${PROTOCOL:-ipv4}
if [ "$PROTOCOL" = "ipv4" ]; then
  TS_CIDR=${TS_CIDR:-169.254.100.0/24}
elif [ "$PROTOCOL" = "ipv6" ]; then
  TS_CIDR=${TS_CIDR:-fe80:a9fe:64::/112}
elif [ "$PROTOCOL" = "dual" ]; then
  TS_CIDR=${TS_CIDR:-"169.254.100.0/24,fe80:a9fe:64::/112"}
fi

function quit {
    /usr/share/ovn/scripts/ovn-ctl stop_ic_ovsdb
    exit 0
}

function gen_conn_str {
    t=$(echo -n "${NODE_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    x=$(for i in ${t}; do echo -n "tcp:[$i]:$1",; done| sed 's/,$//')
    echo "$x"
}

trap quit EXIT
if [[ -z "$NODE_IPS" && -z "$LOCAL_IP" ]]; then
    /usr/share/ovn/scripts/ovn-ctl --db-ic-nb-create-insecure-remote=yes --db-ic-sb-create-insecure-remote=yes --db-ic-nb-addr="[::]" --db-ic-sb-addr="[::]" start_ic_ovsdb
    /usr/share/ovn/scripts/ovn-ctl status_ic_ovsdb
    ovn-ic-nbctl --may-exist ts-add "$TS_NAME"
    ovn-ic-nbctl set Transit_Switch ts external_ids:subnet="$TS_CIDR"
    tail --follow=name --retry /var/log/ovn/ovsdb-server-ic-nb.log
else
    if [[ -z "$LEADER_IP" ]]; then
        echo "leader start with local ${LOCAL_IP} and cluster $(gen_conn_str 6647)"
        /usr/share/ovn/scripts/ovn-ctl  --db-ic-nb-create-insecure-remote=yes \
        --db-ic-sb-create-insecure-remote=yes \
        --db-ic-sb-cluster-local-addr="${LOCAL_IP}" \
        --db-ic-nb-cluster-local-addr="${LOCAL_IP}" \
        --ovn-ic-nb-db="$(gen_conn_str 6647)" \
        --ovn-ic-sb-db="$(gen_conn_str 6648)" \
        start_ic_ovsdb
        /usr/share/ovn/scripts/ovn-ctl status_ic_ovsdb
        ovn-ic-nbctl --may-exist ts-add "$TS_NAME"
        ovn-ic-nbctl set Transit_Switch ts external_ids:subnet="$TS_CIDR"
        tail --follow=name --retry /var/log/ovn/ovsdb-server-ic-nb.log
    else
        echo "follower start with local ${LOCAL_IP}, leader ${LEADER_IP} and cluster $(gen_conn_str 6647)"
        /usr/share/ovn/scripts/ovn-ctl  --db-ic-nb-create-insecure-remote=yes \
        --db-ic-sb-create-insecure-remote=yes \
        --db-ic-sb-cluster-local-addr="${LOCAL_IP}" \
        --db-ic-nb-cluster-local-addr="${LOCAL_IP}" \
        --db-ic-nb-cluster-remote-addr="${LEADER_IP}" \
        --db-ic-sb-cluster-remote-addr="${LEADER_IP}" \
        --ovn-ic-nb-db="$(gen_conn_str 6647)" \
        --ovn-ic-sb-db="$(gen_conn_str 6648)" \
        start_ic_ovsdb
        tail --follow=name --retry /var/log/ovn/ovsdb-server-ic-nb.log
    fi
fi
