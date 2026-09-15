#!/usr/bin/env bash
set -Eeuo pipefail

script="$(cd "$(dirname "$0")" && pwd)/nat-gateway.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

# Keep the script from reading an interface file left behind on the host.
export NAT_GW_ENV_FILE="$tmp_dir/nat-gateway.env"

# Load definitions without executing the command dispatcher at the end.
eval "$(sed '/^opt=\$1$/,$d' "$script")"

VPC_INTERFACE=eth0
vpc_addr=10.0.7.254
vpc_has_addr=true

iptables_cmd=iptables
iptables_save_cmd=iptables_save_stub
# check_inited greps the save output for these two chain markers.
iptables_save_stub() { printf 'SNAT_FILTER SHARED_SNAT\n'; }

lo_addrs="$tmp_dir/lo.addrs"
ip_log="$tmp_dir/ip.log"
ipt_log="$tmp_dir/iptables.log"
hairpin_state="$tmp_dir/hairpin.rules"
: > "$lo_addrs"
: > "$ip_log"
: > "$ipt_log"
: > "$hairpin_state"

ip() {
    printf 'ip %s\n' "$*" >> "$ip_log"
    case "$*" in
        "-4 addr show dev $VPC_INTERFACE")
            [[ "$vpc_has_addr" == true ]] && printf 'inet %s/24 scope global %s\n' "$vpc_addr" "$VPC_INTERFACE"
            ;;
        "addr show lo")
            while read -r addr; do printf 'inet %s scope host lo\n' "$addr"; done < "$lo_addrs"
            ;;
        "addr replace "*" dev lo")
            local addr="${3}"
            grep -qxF "$addr" "$lo_addrs" || printf '%s\n' "$addr" >> "$lo_addrs"
            ;;
        "addr del "*" dev lo")
            local addr="${3}"
            grep -vxF "$addr" "$lo_addrs" > "$lo_addrs.new" || true
            mv "$lo_addrs.new" "$lo_addrs"
            ;;
        *)
            echo "unexpected ip invocation: $*" >&2
            return 1
            ;;
    esac
}

# Only the check-and-create/delete forms the VIP helpers use are emulated.
iptables() {
    printf 'iptables %s\n' "$*" >> "$ipt_log"
    local full="$*" op rule
    op="${3}"
    rule="${full#-t nat $op HAIRPIN_SNAT }"
    rule="${rule#1 }"
    rule="${rule% --random-fully}"
    case "$op" in
        -C) grep -qxF -e "$rule" "$hairpin_state" ;;
        -I) grep -qxF -e "$rule" "$hairpin_state" || printf '%s\n' "$rule" >> "$hairpin_state" ;;
        -D)
            grep -vxF -e "$rule" "$hairpin_state" > "$hairpin_state.new" || true
            mv "$hairpin_state.new" "$hairpin_state"
            ;;
        *)
            echo "unexpected iptables invocation: $*" >&2
            return 1
            ;;
    esac
}

# The ClusterIP is held on lo as a single /32 so the gateway owns the VIP without
# claiming a whole ClusterIP range on the VPC interface.
vip_addr_add 10.96.1.5
grep -qxF '10.96.1.5/32' "$lo_addrs"
grep -qF 'ip addr replace 10.96.1.5/32 dev lo' "$ip_log"
# re-running must converge, not stack addresses
vip_addr_add 10.96.1.5
[[ "$(wc -l < "$lo_addrs")" == 1 ]]
vip_addr_del 10.96.1.5
[[ ! -s "$lo_addrs" ]]
# deleting an address that is not held must not call ip addr del
del_calls="$(grep -c 'addr del' "$ip_log")"
vip_addr_del 10.96.1.5
[[ "$(grep -c 'addr del' "$ip_log")" == "$del_calls" ]]

# Hairpin SNAT: both the internal (ClusterIP) and public (EIP) VIP of one service get a
# per-identity rule that SNATs VPC-originated traffic to this gateway's own VPC address,
# so the backend reply returns to the replica holding the conntrack.
vip_hairpin_add '10.96.1.5,80,tcp'
vip_hairpin_add '203.0.113.10,80,tcp'
[[ "$(wc -l < "$hairpin_state")" == 2 ]]
grep -qF -- '-m mark --mark 0x1/0x1 -o eth0 -p tcp -m conntrack --ctstate DNAT --ctorigdst 10.96.1.5 --ctorigdstport 80 -j SNAT --to-source 10.0.7.254' "$hairpin_state"
grep -qF -- '--ctorigdst 203.0.113.10 --ctorigdstport 80 -j SNAT --to-source 10.0.7.254' "$hairpin_state"
# inserted at the head, otherwise the VIP-wide rule from eip-add (SNAT to the EIP) would win
grep -qF 'iptables -t nat -I HAIRPIN_SNAT 1 ' "$ipt_log"
# repeated add keeps exactly one rule per identity
vip_hairpin_add '10.96.1.5,80,tcp'
[[ "$(wc -l < "$hairpin_state")" == 2 ]]
# deletion is idempotent and identity-scoped
vip_hairpin_del '10.96.1.5,80,tcp'
[[ "$(wc -l < "$hairpin_state")" == 1 ]]
grep -qF -- '--ctorigdst 203.0.113.10' "$hairpin_state"
vip_hairpin_del '10.96.1.5,80,tcp'
[[ "$(wc -l < "$hairpin_state")" == 1 ]]
# a second port of the same VIP is a distinct identity
vip_hairpin_add '10.96.1.5,443,udp'
grep -qF -- '--ctorigdst 10.96.1.5 --ctorigdstport 443 -j SNAT --to-source 10.0.7.254' "$hairpin_state"
# sctp is a first class protocol next to tcp/udp (kube-proxy parity)
vip_hairpin_add '10.96.1.5,132,sctp'
grep -qF -- '--ctorigdst 10.96.1.5 --ctorigdstport 132 -j SNAT --to-source 10.0.7.254' "$hairpin_state"
vip_hairpin_del '10.96.1.5,132,sctp'
! grep -qF -- '--ctorigdstport 132' "$hairpin_state"
# the protocol is normalized, so the controller may pass either case
vip_hairpin_add '10.96.1.5,8443,TCP'
grep -qF -- '--ctorigdst 10.96.1.5 --ctorigdstport 8443 -j SNAT --to-source 10.0.7.254' "$hairpin_state"
vip_hairpin_del '10.96.1.5,8443,TCP'
! grep -qF -- '--ctorigdstport 8443' "$hairpin_state"

# Invalid input must fail instead of interpolating into the iptables command line.
! ( vip_hairpin_add '10.96.1.5,70000,tcp' ) 2>/dev/null
! ( vip_hairpin_add 'not-an-ip,80,tcp' ) 2>/dev/null
! ( vip_hairpin_add '10.96.1.5,80,icmp' ) 2>/dev/null

# Without an address on the VPC interface the SNAT source is unknown: fail loudly.
vpc_has_addr=false
! ( vip_hairpin_add '10.96.1.5,80,tcp' ) 2>/dev/null
vpc_has_addr=true

echo 'PASS: share-DNAT VIP address and hairpin rules'
