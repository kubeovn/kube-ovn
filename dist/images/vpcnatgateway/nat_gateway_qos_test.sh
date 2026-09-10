#!/usr/bin/env bash
set -Eeuo pipefail

script="$(cd "$(dirname "$0")" && pwd)/nat-gateway.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

# Load definitions without executing the command dispatcher at the end.
eval "$(sed '/^opt=\$1$/,$d' "$script")"

# This address hashes to decimal 9999 (hex 0x270f). It must remain available:
# tc's textual class 1:9999 is hexadecimal 0x9999, outside the EIP range.
[[ "$(ip_to_classid 0.1.34.7)" == "0x270f" ]]
# This CIDR hashes to the shared default 0x9999 and must be remapped within
# the NAT gateway range instead of replacing the default class.
[[ "$(cidr_to_classid 0.0.115.242/32 1)" == "0xfeff" ]]

tc_log="$tmp_dir/tc.log"
scenario=overlap
tc() {
    if [[ "$1 $2 $3 $4 $5" == "-p filter show dev net1" ]]; then
        [[ "$scenario" == filter-show-failure ]] && return 1
        case "$scenario" in
            delete-failure)
                printf 'filter parent 1: protocol ip pref 2 u32 fh 270::800 flowid 1:270f\n'
                printf '  match IP src 192.0.2.1/32\n'
                ;;
            collision)
                printf 'filter parent 1: protocol ip pref 1 u32 fh 100::800 flowid 1:100\n'
                printf '  match IP src 198.51.100.1/32\n'
                printf 'filter parent 1: protocol ip pref 2 u32 fh 800::800 flowid 1:8000\n'
                printf '  match IP src 198.51.100.0/24\n'
                ;;
            priority)
                printf 'filter parent 1: protocol ip pref 1 u32 fh 800::800 flowid 1:8000\n'
                printf '  match IP src 192.0.2.0/24\n'
                ;;
            overlap)
                printf 'filter parent 1: protocol ip pref 1 u32 fh 800::800 flowid 1:8000\n'
                printf '  match IP src 192.0.2.1/32\n'
                printf 'filter parent 1: protocol ip pref 3 u32 fh 801::800 flowid 1:8001\n'
                printf '  match IP src 192.0.2.1/32\n'
                printf 'filter parent 1: protocol ip pref 2 u32 fh 270::800 flowid 1:270f\n'
                printf '  match IP src 192.0.2.1/32\n'
                ;;
            default)
                printf 'filter parent 1: protocol ip pref 1 u32 fh 999::800 flowid 1:9999\n'
                printf '  match IP src 192.0.2.1/32\n'
                ;;
            legacy-priority)
                # A rule configured with priority 0 is stored by tc as pref 49152.
                printf 'filter parent 1: protocol ip pref 49152 u32 fh 800::800 flowid 1:8000\n'
                printf '  match IP src 192.0.2.1/32\n'
                ;;
            ambiguous-priority)
                # Same identity, two rules, neither at the requested priority.
                printf 'filter parent 1: protocol ip pref 49152 u32 fh 800::800 flowid 1:8000\n'
                printf '  match IP src 192.0.2.1/32\n'
                printf 'filter parent 1: protocol ip pref 49153 u32 fh 801::800 flowid 1:8001\n'
                printf '  match IP src 192.0.2.1/32\n'
                ;;
            deleted)
                # The filter is already gone, so there is no candidate at all.
                ;;
        esac
        return
    fi
    if [[ "$1 $2 $3 $4" == "filter show dev net1" ]]; then
        [[ "$scenario" == matchall-show-failure ]] && return 1
        if [[ "$scenario" == matchall-filter-failure ]]; then
            printf 'filter protocol ip pref 3 matchall handle 0xff03 flowid 1:ff03\n'
        fi
        return
    fi
    if [[ "$1 $2 $3" == "class show dev" ]]; then
        [[ "$scenario" == class-show-failure ]] && return 1
        if [[ "$scenario" == collision ]]; then
            printf 'class htb 1:100 parent 1:\n'
        elif [[ "$scenario" == matchall-class-failure ]]; then
            printf 'class htb 1:ff03 parent 1:\n'
        fi
        return
    fi
    printf '%s\n' "$*" >>"$tc_log"
    if [[ "$scenario" == *-failure && "$1 $2" == "filter del" ||
        "$scenario" == matchall-class-failure && "$1 $2" == "class del" ]]; then
        return 1
    fi
}

delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src eip
grep -q '^filter del dev net1 parent 1: prio 2 handle 270::800 u32$' "$tc_log"
grep -q '^class del dev net1 classid 1:0x270f$' "$tc_log"
! grep -q 'handle 800::800' "$tc_log"

: >"$tc_log"
delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src natgw 1
grep -q '^filter del dev net1 parent 1: prio 1 handle 800::800 u32$' "$tc_log"
grep -q '^class del dev net1 classid 1:0x8000$' "$tc_log"
! grep -q 'handle 270::800' "$tc_log"
! grep -q 'handle 801::800' "$tc_log"

: >"$tc_log"
scenario=default
delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src eip
[[ ! -s "$tc_log" ]]

# A natgw rule whose configured priority 0 was rewritten by tc must still be
# removable, and only when a single filter matches it.
: >"$tc_log"
scenario=legacy-priority
delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src natgw 0
grep -q '^filter del dev net1 parent 1: prio 49152 handle 800::800 u32$' "$tc_log"
grep -q '^class del dev net1 classid 1:0x8000$' "$tc_log"

: >"$tc_log"
scenario=ambiguous-priority
delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src natgw 0
[[ ! -s "$tc_log" ]]

# Re-deleting a rule whose filter is already gone stays a no-op instead of
# falling back to a filter it cannot identify.
: >"$tc_log"
scenario=deleted
delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src natgw 1
delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src eip
[[ ! -s "$tc_log" ]]

scenario=priority
[[ "$(find_available_classid_for_cidr net1 0x8000 192.0.2.0/24 src 1)" == "0x8000" ]]
classid=$(find_available_classid_for_cidr net1 0x8000 192.0.2.0/24 src 2)
[[ "$classid" != "0x8000" ]]

scenario=collision
[[ "$(find_available_classid net1 0x100 198.51.100.1 src)" == "0x100" ]]
! classid=$(find_available_classid net1 0x100 192.0.2.1 src 2>"$tmp_dir/eip-error")
[[ -z "$classid" ]]
grep -q 'no available EIP QoS classid' "$tmp_dir/eip-error"
! classid=$(find_available_classid_for_cidr net1 0x8000 192.0.2.0/24 src 1 2>"$tmp_dir/natgw-error")
[[ -z "$classid" ]]
grep -q 'no available NAT gateway QoS classid' "$tmp_dir/natgw-error"

: >"$tc_log"
scenario=delete-failure
! delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src eip 2>"$tmp_dir/delete-error"
grep -q 'failed to delete QoS filter' "$tmp_dir/delete-error"
! grep -q '^class del ' "$tc_log"

: >"$tc_log"
scenario=matchall-filter-failure
! delete_htb_matchall_filter_and_class net1 0xff03 2>"$tmp_dir/matchall-filter-error"
grep -q 'failed to delete matchall filter' "$tmp_dir/matchall-filter-error"
! grep -q '^class del ' "$tc_log"

: >"$tc_log"
scenario=matchall-class-failure
! delete_htb_matchall_filter_and_class net1 0xff03 2>"$tmp_dir/matchall-class-error"
grep -q 'failed to delete matchall class' "$tmp_dir/matchall-class-error"

scenario=filter-show-failure
! delete_htb_filter_and_class net1 '192\.0\.2\.1/32' src eip 2>"$tmp_dir/filter-show-error"
grep -q 'failed to list QoS filters' "$tmp_dir/filter-show-error"

scenario=matchall-show-failure
! delete_htb_matchall_filter_and_class net1 0xff03 2>"$tmp_dir/matchall-show-error"
grep -q 'failed to list matchall filters' "$tmp_dir/matchall-show-error"

scenario=class-show-failure
! delete_htb_matchall_filter_and_class net1 0xff03 2>"$tmp_dir/class-show-error"
grep -q 'failed to list matchall class' "$tmp_dir/class-show-error"

# A restarted vpc-nat-gw container loses its writable layer: the iptables chains
# (in the pod netns) survive, but the persisted interfaces do not. Re-running init
# must rewrite them before it exits early on the existing chains.
export NAT_GW_ENV_FILE="$tmp_dir/nat-gateway.env"
ip() { return 0; }
iptables_cmd=true
iptables_save_cmd=iptables_save_stub
iptables_save_stub() { printf 'DNAT_FILTER\n'; }
( init "net1,net1" )
grep -q '^VPC_INTERFACE=net1$' "$NAT_GW_ENV_FILE"
grep -q '^EXTERNAL_INTERFACE=net1$' "$NAT_GW_ENV_FILE"

echo 'PASS: QoS class ranges isolate EIP and shared state'
