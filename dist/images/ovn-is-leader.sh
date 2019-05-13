#!/bin/bash
set -euo pipefail

alias ovn-ctl='/usr/share/openvswitch/scripts/ovn-ctl'

ovn-ctl status_northd
ovn-ctl status_ovnnb
ovn-ctl status_ovnsb

# For data consistency, only store leader address in endpoint
echo $(cat /var/log/openvswitch/ovn-northd.log) | grep -oP "lock acquired(?!.*lock lost).*$"
