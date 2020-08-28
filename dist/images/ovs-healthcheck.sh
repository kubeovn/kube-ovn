#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

echo Connecting OVN SB "${OVN_SB_SERVICE_HOST}":"${OVN_SB_SERVICE_PORT}"
ovn-sbctl --db=tcp:["${OVN_SB_SERVICE_HOST}"]:"${OVN_SB_SERVICE_PORT}" --timeout=15 show

alias ovs-ctl='/usr/share/openvswitch/scripts/ovs-ctl'
alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ovs-ctl status
ovn-ctl status_controller
