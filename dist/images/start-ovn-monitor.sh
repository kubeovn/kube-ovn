#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}

if [[ "$ENABLE_SSL" == "false" ]]; then
  export OVN_NB_DAEMON=$(ovn-nbctl --db=tcp:["${OVN_NB_SERVICE_HOST}"]:"${OVN_NB_SERVICE_PORT}" --pidfile --detach --overwrite-pidfile)
else
  export OVN_NB_DAEMON=$(ovn-nbctl -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert --db=ssl:["${OVN_NB_SERVICE_HOST}"]:"${OVN_NB_SERVICE_PORT}" --pidfile --detach --overwrite-pidfile)
fi

exec ./kube-ovn-monitor $@
