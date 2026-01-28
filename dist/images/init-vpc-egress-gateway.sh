#!/bin/bash

set -ex

INTERNAL_GATEWAY_IPV4=${INTERNAL_GATEWAY_IPV4:-}
INTERNAL_GATEWAY_IPV6=${INTERNAL_GATEWAY_IPV6:-}
EXTERNAL_GATEWAY_IPV4=${EXTERNAL_GATEWAY_IPV4:-}
EXTERNAL_GATEWAY_IPV6=${EXTERNAL_GATEWAY_IPV6:-}
NO_SNAT_SOURCES_IPV4=($(echo "${NO_SNAT_SOURCES_IPV4:-}" | tr ',' ' '))
NO_SNAT_SOURCES_IPV6=($(echo "${NO_SNAT_SOURCES_IPV6:-}" | tr ',' ' '))
ENABLE_BGP=${ENABLE_BGP:-}
ENABLE_EVPN=${ENABLE_EVPN:-}
VNI=${VNI:-}

sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1

iptables -V

internal_iface="eth0"
external_iface="net1"
masquerade_chain="VEG-MASQUERADE"

if [ "${ENABLE_BGP}" != "true" ]; then
  if [ -n "${INTERNAL_GATEWAY_IPV4}" ]; then
    internal_ipv4=`ip -o route get "${INTERNAL_GATEWAY_IPV4}" | grep -o 'src [^ ]*' | awk '{print $2}'`
    external_ipv4=`ip -o route get "${EXTERNAL_GATEWAY_IPV4}" | grep -o 'src [^ ]*' | awk '{print $2}'`
    internal_iface=`ip -o route get "${INTERNAL_GATEWAY_IPV4}" | grep -o 'dev [^ ]*' | awk '{print $2}'`
    external_iface=`ip -o route get "${EXTERNAL_GATEWAY_IPV4}" | grep -o 'dev [^ ]*' | awk '{print $2}'`
    ip -4 route replace default via "${INTERNAL_GATEWAY_IPV4}" table 1000
    for priority in 1001 1002 1003 1004; do
      if [ -n "`ip -4 rule show priority ${priority}`" ]; then
        ip -4 rule del priority "${priority}"
      fi
    done
    ip -4 rule add priority 1001 iif "${internal_iface}" lookup default
    ip -4 rule add priority 1002 iif "${external_iface}" lookup 1000
    ip -4 rule add priority 1003 iif lo from "${internal_ipv4}" lookup 1000
    ip -4 rule add priority 1004 iif lo from "${external_ipv4}" lookup default

    if ! iptables -t nat -S ${masquerade_chain} 1 &>/dev/null; then
      iptables -t nat -N ${masquerade_chain}
    fi
    iptables -t raw -F PREROUTING
    iptables -t nat -F PREROUTING
    iptables -t nat -F POSTROUTING
    iptables -t nat -F ${masquerade_chain}
    iptables -t nat -A PREROUTING -i ${internal_iface} -j MARK --set-xmark 0x4000/0x4000
    iptables -t nat -A POSTROUTING -j ${masquerade_chain}
    iptables -t nat -A ${masquerade_chain} -j MARK --set-xmark 0x0/0xffffffff
    iptables -t nat -A ${masquerade_chain} -j MASQUERADE --random-fully
    for src in ${NO_SNAT_SOURCES_IPV4[*]}; do
      iptables -t nat -I POSTROUTING -s "${src}" -j RETURN
      iptables -t nat -I POSTROUTING -d "${src}" -j RETURN
    done
  fi

  if [ -n "${INTERNAL_GATEWAY_IPV6}" ]; then
    internal_ipv6=`ip -o route get "${INTERNAL_GATEWAY_IPV6}" | grep -o 'src [^ ]*' | awk '{print $2}'`
    external_ipv6=`ip -o route get "${EXTERNAL_GATEWAY_IPV6}" | grep -o 'src [^ ]*' | awk '{print $2}'`
    internal_iface=`ip -o route get "${INTERNAL_GATEWAY_IPV6}" | grep -o 'dev [^ ]*' | awk '{print $2}'`
    external_iface=`ip -o route get "${EXTERNAL_GATEWAY_IPV6}" | grep -o 'dev [^ ]*' | awk '{print $2}'`
    ip -6 route replace default via "${INTERNAL_GATEWAY_IPV6}" table 1000
    for priority in 1001 1002 1003 1004; do
      if [ -n "`ip -6 rule show priority ${priority}`" ]; then
        ip -6 rule del priority "${priority}"
      fi
    done
    ip -6 rule add priority 1001 iif "${internal_iface}" lookup default
    ip -6 rule add priority 1002 iif "${external_iface}" lookup 1000
    ip -6 rule add priority 1003 iif lo from "${internal_ipv6}" lookup 1000
    ip -6 rule add priority 1004 iif lo from "${external_ipv6}" lookup default

    if ! ip6tables -t nat -S ${masquerade_chain} 1 &>/dev/null; then
      ip6tables -t nat -N ${masquerade_chain}
    fi
    ip6tables -t raw -F PREROUTING
    ip6tables -t nat -F PREROUTING
    ip6tables -t nat -F POSTROUTING
    ip6tables -t nat -F ${masquerade_chain}
    ip6tables -t nat -A PREROUTING -i ${internal_iface} -j MARK --set-xmark 0x4000/0x4000
    ip6tables -t nat -A POSTROUTING -j ${masquerade_chain}
    ip6tables -t nat -A ${masquerade_chain} -j MARK --set-xmark 0x0/0xffffffff
    ip6tables -t nat -A ${masquerade_chain} -j MASQUERADE --random-fully
    for src in ${NO_SNAT_SOURCES_IPV6[*]}; do
      ip6tables -t nat -I POSTROUTING -s "${src}" -j RETURN
      ip6tables -t nat -I POSTROUTING -d "${src}" -j RETURN
    done
  fi
fi

sysctl net/ipv4/conf/${internal_iface}/rp_filter=0

if [ "${ENABLE_EVPN}" = "true" ] && [ -n "${VNI}" ]; then
  vrf_name="vrf-vpn"
  bridge_name="br-vpn"
  vxlan_name="vxlan-vpn"
  vrf_table=2000

  if ! ip link show "${vrf_name}" &>/dev/null; then
    ip link add "${vrf_name}" type vrf table "${vrf_table}"
    ip link set "${vrf_name}" up
  fi

  if ! ip link show "${bridge_name}" &>/dev/null; then
    ip link add "${bridge_name}" type bridge
    ip link set "${bridge_name}" master "${vrf_name}"
    ip link set "${bridge_name}" up
  fi

  if ! ip link show "${vxlan_name}" &>/dev/null; then
    vxlan_local_ip=$(ip -4 -o addr show dev "${external_iface}" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1)
    if [ -z "${vxlan_local_ip}" ]; then
      vxlan_local_ip=$(ip -6 -o addr show dev "${external_iface}" 2>/dev/null | awk '{print $4}' | cut -d/ -f1 | head -1)
    fi
    if [ -n "${vxlan_local_ip}" ]; then
      ip link add "${vxlan_name}" type vxlan id "${VNI}" dstport 4789 local "${vxlan_local_ip}"
      ip link set "${vxlan_name}" master "${bridge_name}"
      ip link set "${vxlan_name}" up
    fi
  fi

  if [ "$(ip link show dev eth0 | grep -c 'master vrf-vpn')" -eq 0 ]; then
    ip link set eth0 master "${vrf_name}"
  fi

  for src in ${NO_SNAT_SOURCES_IPV4[*]}; do
    if [ -z "$(ip route show "${src}" table "${vrf_table}" 2>/dev/null)" ]; then
      ip route add "${src}" via "${INTERNAL_GATEWAY_IPV4}" dev eth0 table "${vrf_table}"
    fi
  done
  for src in ${NO_SNAT_SOURCES_IPV6[*]}; do
    if [ -z "$(ip -6 route show "${src}" table "${vrf_table}" 2>/dev/null)" ]; then
      ip -6 route add "${src}" via "${INTERNAL_GATEWAY_IPV6}" dev eth0 table "${vrf_table}"
    fi
  done
fi
