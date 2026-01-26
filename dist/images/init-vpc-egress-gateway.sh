#!/bin/bash

set -ex

INTERNAL_GATEWAY_IPV4=${INTERNAL_GATEWAY_IPV4:-}
INTERNAL_GATEWAY_IPV6=${INTERNAL_GATEWAY_IPV6:-}
EXTERNAL_GATEWAY_IPV4=${EXTERNAL_GATEWAY_IPV4:-}
EXTERNAL_GATEWAY_IPV6=${EXTERNAL_GATEWAY_IPV6:-}
NO_SNAT_SOURCES_IPV4=($(echo "${NO_SNAT_SOURCES_IPV4:-}" | tr ',' ' '))
NO_SNAT_SOURCES_IPV6=($(echo "${NO_SNAT_SOURCES_IPV6:-}" | tr ',' ' '))
ENABLE_BGP=${ENABLE_BGP:-}

sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1

iptables -V

internal_iface="eth0"
external_iface="net1"
masquerade_chain="VEG-MASQUERADE"

if [ -n "${INTERNAL_GATEWAY_IPV4}" ]; then
  # ip rules and routes for IPv4
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
  # packets from internal networks
  ip -4 rule add priority 1001 iif "${internal_iface}" lookup default
  # packets from external networks
  ip -4 rule add priority 1002 iif "${external_iface}" lookup 1000
  # response packets to internal networks
  ip -4 rule add priority 1003 iif lo from "${internal_ipv4}" lookup 1000
  # response packets to external networks
  ip -4 rule add priority 1004 iif lo from "${external_ipv4}" lookup default

  if [ "${ENABLE_BGP}" != "true" ]; then
    # iptables rules for IPv4
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
fi

if [ -n "${INTERNAL_GATEWAY_IPV6}" ]; then
  # ip rules and routes for IPv6
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
  # packets from internal networks
  ip -6 rule add priority 1001 iif "${internal_iface}" lookup default
  # packets from external networks
  ip -6 rule add priority 1002 iif "${external_iface}" lookup 1000
  # response packets to internal networks
  ip -6 rule add priority 1003 iif lo from "${internal_ipv6}" lookup 1000
  # response packets to external networks
  ip -6 rule add priority 1004 iif lo from "${external_ipv6}" lookup default

  if [ "${ENABLE_BGP}" != "true" ]; then
    # iptables rules for IPv6
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
