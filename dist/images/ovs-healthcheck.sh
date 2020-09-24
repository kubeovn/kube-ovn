#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

echo Connecting OVN SB "${OVN_SB_SERVICE_HOST}":"${OVN_SB_SERVICE_PORT}"
if [[ "$ENABLE_SSL" == "false" ]]; then
  ovn-sbctl --db=tcp:["${OVN_SB_SERVICE_HOST}"]:"${OVN_SB_SERVICE_PORT}" --timeout=15 show
else
  ovn-sbctl -p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert --db=ssl:["${OVN_SB_SERVICE_HOST}"]:"${OVN_SB_SERVICE_PORT}" --timeout=15 show
fi
alias ovs-ctl='/usr/share/openvswitch/scripts/ovs-ctl'
alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ovs-ctl status
ovn-ctl status_controller
