#!/usr/bin/env bash
set -euo pipefail
export OVN_NB_DAEMON=$(ovn-nbctl --db=tcp:["${OVN_NB_SERVICE_HOST}"]:"${OVN_NB_SERVICE_PORT}" --pidfile --detach --overwrite-pidfile)
exec ./kube-ovn-controller --ovn-nb-host="${OVN_NB_SERVICE_HOST}" \
                           --ovn-nb-port="${OVN_NB_SERVICE_PORT}" \
                           --ovn-sb-host="${OVN_SB_SERVICE_HOST}" \
                           --ovn-sb-port="${OVN_SB_SERVICE_PORT}" \
                           $@
