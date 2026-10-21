#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
script=${script_dir}/init-vpc-egress-gateway.sh

keep_addr_line=$(grep -n '^  sysctl -w net\.ipv6\.conf\.all\.keep_addr_on_down=1$' "${script}" | cut -d: -f1)
vrf_placeholder="\${vrf_name}"
enslave_line=$(grep -nF "    ip link set eth0 master \"${vrf_placeholder}\"" "${script}" | cut -d: -f1)
nolearning_line=$(grep -nF "dstport 4789 local \"\${vxlan_local_ip}\" nolearning" "${script}" | cut -d: -f1)
bridge_learning_line=$(grep -nF '  bridge link set dev "${vxlan_name}" learning off' "${script}" | cut -d: -f1)
existing_vxlan_config_line=$(grep -nF '  if ip link show "${vxlan_name}" &>/dev/null; then' "${script}" | cut -d: -f1)
runtime_nolearning_line=$(grep -nF '    ip link set "${vxlan_name}" type vxlan nolearning' "${script}" | cut -d: -f1)

if [[ -z "${keep_addr_line}" || -z "${enslave_line}" || "${keep_addr_line}" -ge "${enslave_line}" ]]; then
  printf 'IPv6 keep_addr_on_down must be configured before eth0 is enslaved to the EVPN VRF\n' >&2
  exit 1
fi

if [[ -z "${nolearning_line}" || "${nolearning_line}" -ge "${enslave_line}" ]]; then
  printf 'EVPN VXLAN must disable kernel learning before the interface is attached to the VRF\n' >&2
  exit 1
fi

if [[ -z "${bridge_learning_line}" || "${bridge_learning_line}" -ge "${enslave_line}" ]]; then
  printf 'EVPN VXLAN bridge port must disable dynamic MAC learning before the interface is attached to the VRF\n' >&2
  exit 1
fi

if [[ -z "${existing_vxlan_config_line}" || -z "${runtime_nolearning_line}" || "${existing_vxlan_config_line}" -ge "${enslave_line}" || "${runtime_nolearning_line}" -ge "${enslave_line}" ]]; then
  printf 'EVPN VXLAN learning configuration must also run for an existing interface\n' >&2
  exit 1
fi

printf 'EVPN IPv6 VRF setup preserves addresses before enslavement\n'
