#!/bin/bash
set -euo pipefail

alias ovs-ctl='/usr/share/openvswitch/scripts/ovs-ctl'
alias ovn-ctl='/usr/share/openvswitch/scripts/ovn-ctl'

ovs-ctl status
ovn-ctl status_controller
ovn-ctl status_controller_vtep
