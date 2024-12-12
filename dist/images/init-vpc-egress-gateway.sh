#!/bin/bash

set -ex

INTERNAL_GATEWAY_IPV4=${INTERNAL_GATEWAY_IPV4:-}
INTERNAL_GATEWAY_IPV6=${INTERNAL_GATEWAY_IPV6:-}
INTERNAL_ROUTE_DST_IPV4=($(echo "${INTERNAL_ROUTE_DST_IPV4:-}" | tr ',' ' '))
INTERNAL_ROUTE_DST_IPV6=($(echo "${INTERNAL_ROUTE_DST_IPV6:-}" | tr ',' ' '))
SNAT_SOURCES_IPV4=($(echo "${SNAT_SOURCES_IPV4:-}" | tr ',' ' '))
SNAT_SOURCES_IPV6=($(echo "${SNAT_SOURCES_IPV6:-}" | tr ',' ' '))

sysctl -w net.ipv4.ip_forward=1
sysctl -w net.ipv6.conf.all.forwarding=1

iptables -V

for dst in ${INTERNAL_ROUTE_DST_IPV4[*]}; do 
  ip route add "${dst}" via "${INTERNAL_GATEWAY_IPV4}"
done

for dst in ${INTERNAL_ROUTE_DST_IPV6[*]}; do 
  ip route add "${dst}" via "${INTERNAL_GATEWAY_IPV6}"
done

for src in ${SNAT_SOURCES_IPV4[*]}; do 
  iptables -t nat -A POSTROUTING -s "${src}" -j MASQUERADE --random-fully
done

for src in ${SNAT_SOURCES_IPV6[*]}; do
  ip6tables -t nat -A POSTROUTING -s "${src}" -j MASQUERADE --random-fully
done
