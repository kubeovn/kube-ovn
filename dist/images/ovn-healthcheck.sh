#!/bin/bash
set -euo pipefail

alias ovn-ctl='/usr/share/openvswitch/scripts/ovn-ctl'

ovn-ctl status_northd
ovn-ctl status_ovnnb
ovn-ctl status_ovnsb
