#!/bin/bash
set -euo pipefail
shopt -s expand_aliases

alias ovs-ctl='/usr/share/openvswitch/scripts/ovs-ctl'
alias ovn-ctl='/usr/share/ovn/scripts/ovn-ctl'

ovs-ctl status
ovn-ctl status_controller
