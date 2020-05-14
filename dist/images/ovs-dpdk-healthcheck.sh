#!/bin/bash
set -euo pipefail

ovs-ctl status
ovs-vsctl get Open_vSwitch . dpdk_initialized
ovn-ctl status_controller
