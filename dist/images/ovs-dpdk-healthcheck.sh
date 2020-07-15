#!/bin/bash
set -euo pipefail

echo Connecting OVN SB "${OVN_SB_SERVICE_HOST}":"${OVN_SB_SERVICE_PORT}"
ovn-sbctl --db=tcp:["${OVN_SB_SERVICE_HOST}"]:"${OVN_SB_SERVICE_PORT}" --timeout=3 show

ovs-ctl status
ovs-vsctl get Open_vSwitch . dpdk_initialized
ovn-ctl status_controller
