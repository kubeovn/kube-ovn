#!/bin/bash
set -euo pipefail

ovn-nbctl show
# wait 5 seconds
ovn-sbctl -t 5 show

# For data consistency, only store leader address in endpoint
echo $(cat /var/log/openvswitch/ovn-northd.log) | grep -oP "lock acquired(?!.*lock lost).*$"