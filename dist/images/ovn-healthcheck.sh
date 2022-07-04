#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ovn-ctl status_northd
ovn-ctl status_ovnnb | grep -q '^running'
ovn-ctl status_ovnsb | grep -q '^running'

ovn-appctl -t /var/run/ovn/ovnnb_db.ctl cluster/status OVN_Northbound | grep Status | grep -v "failed"
ovn-appctl -t /var/run/ovn/ovnsb_db.ctl cluster/status OVN_Southbound | grep Status | grep -v "failed"
