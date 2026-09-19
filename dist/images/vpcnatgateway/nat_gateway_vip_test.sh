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
inited_calls=0
# The real check_inited greps the iptables-save output; stub it so the test output stays quiet
# while still proving the VIP commands refuse to run on an uninitialized gateway.
check_inited() { inited_calls=$((inited_calls+1)); }

lo_addrs="$tmp_dir/lo.addrs"
# Addresses that live on lo without this feature's label: the loopback address itself and whatever
# else the gateway or the operator put there. The sync must never touch them.
lo_foreign="$tmp_dir/lo.foreign"
ip_log="$tmp_dir/ip.log"
ipt_log="$tmp_dir/iptables.log"
hairpin_state="$tmp_dir/hairpin.rules"
: > "$lo_addrs"
printf '127.0.0.1/8\n10.99.0.1/32\n' > "$lo_foreign"
: > "$ip_log"
: > "$ipt_log"
: > "$hairpin_state"

ip() {
    printf 'ip %s\n' "$*" >> "$ip_log"
    case "$*" in
        "-4 addr show dev $VPC_INTERFACE")
            [[ "$vpc_has_addr" == true ]] && printf 'inet %s/24 scope global %s\n' "$vpc_addr" "$VPC_INTERFACE"
            ;;
        "-4 addr show dev lo label $VIP_ADDR_LABEL")
            while read -r addr; do printf 'inet %s scope host %s\n' "$addr" "$VIP_ADDR_LABEL"; done < "$lo_addrs"
            ;;
        "-4 addr show dev lo")
            # The unscoped view, so that dropping the label filter from the script is caught here
            # (the sync would then try to release addresses it does not own).
            while read -r addr; do printf 'inet %s scope host %s\n' "$addr" "$VIP_ADDR_LABEL"; done < "$lo_addrs"
            while read -r addr; do printf 'inet %s scope host lo\n' "$addr"; done < "$lo_foreign"
            ;;
        "addr add "*" dev lo label $VIP_ADDR_LABEL")
            local addr="${3}"
            grep -qxF "$addr" "$lo_addrs" || printf '%s\n' "$addr" >> "$lo_addrs"
            ;;
        "addr del "*" dev lo")
            local addr="${3}"
            if grep -qxF "$addr" "$lo_foreign"; then
                echo "refused to delete unlabeled lo address: $addr" >&2
                return 1
            fi
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

# The ClusterIPs are held on lo as single /32s, under a label that scopes the set this feature
# owns, so the sync can release the ones the controller no longer asks for.
vip_addr_sync 10.96.1.5
grep -qxF '10.96.1.5/32' "$lo_addrs"
grep -qF 'ip addr add 10.96.1.5/32 dev lo label lo:ko-vip' "$ip_log"
# re-running must converge, not stack addresses or re-add what is already held
add_calls="$(grep -c 'addr add' "$ip_log")"
vip_addr_sync 10.96.1.5
[[ "$(wc -l < "$lo_addrs")" == 1 ]]
[[ "$(grep -c 'addr add' "$ip_log")" == "$add_calls" ]]
# a VIP that is not in the desired set is released, whoever left it behind: this is what makes the
# lo state converge without tracking which rule last referenced an address
vip_addr_sync 10.96.2.7
grep -qxF '10.96.2.7/32' "$lo_addrs"
! grep -qxF '10.96.1.5/32' "$lo_addrs"
# an empty desired set releases everything this feature owns
vip_addr_sync
[[ ! -s "$lo_addrs" ]]
# ... and nothing else: the label is what scopes the set, so the loopback address and any other
# unlabeled address on lo survive (the ip stub fails the test if one of them is deleted)
[[ "$(wc -l < "$lo_foreign")" == 2 ]]
# ... and then does not call ip addr del again
del_calls="$(grep -c 'addr del' "$ip_log")"
vip_addr_sync
[[ "$(grep -c 'addr del' "$ip_log")" == "$del_calls" ]]
# the VIP set is only programmed on an initialized gateway
[[ "$inited_calls" == 5 ]]
! ( vip_addr_sync 'not-an-ip' ) 2>/dev/null

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
