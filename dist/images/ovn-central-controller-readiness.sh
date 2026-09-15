#!/usr/bin/env bash
# Readiness probe for ovn-central in DYNAMIC_PEERS mode.
#
# Returns 0 only when BOTH NB and SB ovsdb report:
#   Status: cluster member
#   Leader: <known sid> (not "unknown")
#   AND we have committed data: role=leader OR Log range non-trivial
#   (logHigh > logLow, meaning leader replicated AppendEntries to us).
#
# Other pods' ovn-central-controller treats Pod.Ready=true as the
# trustworthy "this peer is in a working cluster" signal, so the probe
# must NOT pass on a stub that ovsdb-server is reporting cluster_member
# optimistically while AddServer is still mid-flight. Without the log
# check, stub-only pods would briefly pass readiness, peers would treat
# them as authoritative, and we'd lose data via case-1 wipe-rejoin.

set -u

check_db() {
    local sock=$1 name=$2 out leader role lo hi
    if ! out=$(ovs-appctl -t "$sock" cluster/status "$name" 2>/dev/null); then
        return 1
    fi
    grep -q '^Status: cluster member' <<<"$out" || return 1
    leader=$(awk '/^Leader:/ {print $2; exit}' <<<"$out")
    [[ -n "$leader" && "$leader" != "unknown" ]] || return 1
    role=$(awk '/^Role:/ {print $2; exit}' <<<"$out")
    if [[ "$role" == "leader" ]]; then
        return 0  # leader's log is committed by definition
    fi
    # Follower: ensure Log range shows committed entries beyond the
    # initial stub. "Log: [N, M]" -- need M > N. Use -F so we work
    # under BusyBox awk (no gawk match()-with-array extension).
    read -r lo hi < <(awk -F'[][, ]+' '/^Log:/ {print $2, $3; exit}' <<<"$out")
    [[ -n "$lo" && -n "$hi" && "$hi" -gt "$lo" ]]
}

check_db /var/run/ovn/ovnnb_db.ctl OVN_Northbound || exit 1
check_db /var/run/ovn/ovnsb_db.ctl OVN_Southbound || exit 1
exit 0
