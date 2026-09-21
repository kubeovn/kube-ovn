#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script=${script_dir}/init-vpc-egress-gateway.sh

keep_addr_line=$(grep -n '^  sysctl -w net\.ipv6\.conf\.all\.keep_addr_on_down=1$' "${script}" | cut -d: -f1)
vrf_placeholder="\${vrf_name}"
enslave_line=$(grep -nF "    ip link set eth0 master \"${vrf_placeholder}\"" "${script}" | cut -d: -f1)

if [[ -z "${keep_addr_line}" || -z "${enslave_line}" || "${keep_addr_line}" -ge "${enslave_line}" ]]; then
  printf 'IPv6 keep_addr_on_down must be configured before eth0 is enslaved to the EVPN VRF\n' >&2
  exit 1
fi

printf 'EVPN IPv6 VRF setup preserves addresses before enslavement\n'
