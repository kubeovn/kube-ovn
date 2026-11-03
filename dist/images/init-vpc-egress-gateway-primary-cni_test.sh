#!/usr/bin/env bash
set -euo pipefail

script_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
command_log=$(mktemp)
trap 'rm -f "${command_log}"' EXIT
export command_log

sysctl() {
  printf 'sysctl %s\n' "$*" >> "${command_log}"
}

ip() {
  printf 'ip %s\n' "$*" >> "${command_log}"
  case "$*" in
    '-4 -o route get 10.16.0.1') printf '10.16.0.1 dev net1 src 10.16.0.10\n' ;;
    '-6 -o route get fd00:10::1') printf 'fd00:10::1 dev net1 src fd00:10::10\n' ;;
    '-4 -o route show default')
      if [ "${PRIMARY_TEST_NO_DEFAULT:-}" != '1' ]; then
        printf 'default via 192.0.2.1 dev eth0\n'
      fi
      ;;
    '-6 -o route show default') printf 'default via 2001:db8::1 dev eth0\n' ;;
    *) return 1 ;;
  esac
}

iptables() {
  if [ "${1:-}" = '-V' ]; then
    printf 'iptables test\n'
    return 0
  fi
  if [ "${3:-}" = '-S' ]; then
    grep -Fq -- "iptables -t nat -N ${4:-}" "${command_log}"
    return
  fi
  if [ "${3:-}" = '-C' ]; then
    grep -Fq -- "iptables -t ${2} -A ${*:4}" "${command_log}"
    return
  fi
  printf 'iptables %s\n' "$*" >> "${command_log}"
}

ip6tables() {
  if [ "${3:-}" = '-S' ]; then
    grep -Fq -- "ip6tables -t nat -N ${4:-}" "${command_log}"
    return
  fi
  if [ "${3:-}" = '-C' ]; then
    grep -Fq -- "ip6tables -t ${2} -A ${*:4}" "${command_log}"
    return
  fi
  printf 'ip6tables %s\n' "$*" >> "${command_log}"
}

export -f sysctl ip iptables ip6tables
export EXTERNAL_NETWORK_MODE=primary-cni
export INTERNAL_GATEWAY_IPV4=10.16.0.1 INTERNAL_GATEWAY_IPV6=fd00:10::1
export NO_SNAT_SOURCES_IPV4=10.20.0.0/16 NO_SNAT_SOURCES_IPV6=fd00:20::/64

bash "${script_dir}/init-vpc-egress-gateway.sh" >/dev/null 2>&1
bash "${script_dir}/init-vpc-egress-gateway.sh" >/dev/null 2>&1

[ "$(grep -Fc -- 'iptables -t mangle -A PREROUTING -i net1 -j MARK --set-xmark 0x4000/0x4000' "${command_log}")" -eq 1 ]
[ "$(grep -Fc -- 'ip6tables -t mangle -A PREROUTING -i net1 -j MARK --set-xmark 0x4000/0x4000' "${command_log}")" -eq 1 ]
[ "$(grep -Fc -- 'iptables -t nat -A POSTROUTING -o eth0 -m mark --mark 0x4000/0x4000 -j VEG-PRIMARY-SNAT' "${command_log}")" -eq 1 ]
[ "$(grep -Fc -- 'ip6tables -t nat -A POSTROUTING -o eth0 -m mark --mark 0x4000/0x4000 -j VEG-PRIMARY-SNAT' "${command_log}")" -eq 1 ]
if grep -Eq -- '(iptables|ip6tables) -t nat -A POSTROUTING -i ' "${command_log}"; then
  printf 'PrimaryCNI init must not use an input interface in POSTROUTING\n' >&2
  exit 1
fi
[ "$(grep -Fc -- 'iptables -t nat -N VEG-PRIMARY-SNAT' "${command_log}")" -eq 1 ]
[ "$(grep -Fc -- 'ip6tables -t nat -N VEG-PRIMARY-SNAT' "${command_log}")" -eq 1 ]
grep -Fq -- 'iptables -t nat -A VEG-PRIMARY-SNAT -s 10.20.0.0/16 -j RETURN' "${command_log}"
grep -Fq -- 'iptables -t nat -A VEG-PRIMARY-SNAT -d 10.20.0.0/16 -j RETURN' "${command_log}"
grep -Fq -- 'ip6tables -t nat -A VEG-PRIMARY-SNAT -s fd00:20::/64 -j RETURN' "${command_log}"
grep -Fq -- 'ip6tables -t nat -A VEG-PRIMARY-SNAT -d fd00:20::/64 -j RETURN' "${command_log}"
grep -Fq -- 'ip6tables -t nat -A VEG-PRIMARY-SNAT -j MASQUERADE --random-fully' "${command_log}"
if grep -Eq -- 'route (replace|add|del) default| -F (PREROUTING|POSTROUTING)| -t raw ' "${command_log}"; then
  printf 'PrimaryCNI init modified the primary route or a shared iptables chain\n' >&2
  exit 1
fi

if PRIMARY_TEST_NO_DEFAULT=1 bash "${script_dir}/init-vpc-egress-gateway.sh" >/dev/null 2>&1; then
  printf 'PrimaryCNI init must fail without a primary default route\n' >&2
  exit 1
fi

printf 'PrimaryCNI init preserves primary routes and NAT chains\n'
