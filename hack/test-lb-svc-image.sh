#!/usr/bin/env bash
set -euo pipefail

if [[ "${1:-}" == --inside ]]; then
    [[ "$(id -u)" == 65532 && "$(id -g)" == 65532 ]]
    [[ "$(awk '/^NoNewPrivs:/ {print $2}' /proc/self/status)" == 1 ]]
    for field in CapInh CapPrm CapEff CapBnd CapAmb; do
        [[ "$(awk -v field="$field:" '$1 == field {print $2}' /proc/self/status)" == 0000000000003000 ]]
    done
    for tool in capsh ip iptables iptables-legacy arping; do
        getcap "$(readlink -f "$(command -v "$tool")")" | grep '=ep$' >/dev/null
    done
    # The general-purpose shell must not carry file capabilities.
    [[ -z "$(getcap /bin/bash)" ]]
    id
    awk '/^Cap|^NoNewPrivs:/' /proc/self/status

    run_lb_svc() {
        local stderr
        stderr=$(mktemp)
        # execNatRules treats stderr as failure even when the command exits zero.
        if bash /kube-ovn/lb-svc.sh "$@" 2>"$stderr" && [[ ! -s "$stderr" ]]; then
            rm "$stderr"
            return 0
        fi
        cat "$stderr" >&2
        rm "$stderr"
        return 1
    }

    address=$(ip -o -4 addr show dev eth0 | awk '{print $4}')
    gateway=$(ip -4 route show default | awk '{print $3}')
    ip link set eth0 down
    ip link set eth0 name net1
    ip link set net1 up
    ip link add eth0 type dummy
    ip addr add 10.0.0.2/24 dev eth0
    ip link set eth0 up
    run_lb_svc init
    run_lb_svc eip-add "$address,$gateway"
    eip=${address%/*}
    run_lb_svc dnat-add "$eip,8080,tcp,10.96.0.10,80,10.0.0.1"
    ip rule show | grep 'iif net1 lookup 100' >/dev/null
    ip route show table 100 | grep "default via $gateway dev net1" >/dev/null
    iptables -t nat -C PREROUTING -p tcp -d "$eip" --dport 8080 -j DNAT --to-destination 10.96.0.10:80
    iptables -t nat -C POSTROUTING -d 10.96.0.10 -j MASQUERADE
    run_lb_svc dnat-del "$eip,8080,tcp,10.96.0.10,80"
    if iptables -t nat -C PREROUTING -p tcp -d "$eip" --dport 8080 -j DNAT --to-destination 10.96.0.10:80 2>/dev/null; then
        echo 'DNAT rule was not removed' >&2
        exit 1
    fi
    # Exercise the legacy applets and the lock shared with root NAT gateways.
    iptables-legacy -t nat -N LB_SVC_TEST
    iptables-legacy-save -t nat | grep LB_SVC_TEST >/dev/null
    iptables-legacy -t nat -X LB_SVC_TEST
    if capsh --has-p=cap_sys_admin; then
        echo 'Unexpected CAP_SYS_ADMIN' >&2
        exit 1
    fi
    echo 'PASS: non-root lb-svc route/EIP/ARP/DNAT and legacy xtables with no_new_privs'
    exit 0
fi

image=${1:?Usage: test-lb-svc-image.sh IMAGE}
network="lb-svc-test-$$-$RANDOM"
container="$network"
cleanup() {
    docker rm -f "$container" >/dev/null 2>&1 || true
    docker network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker network create "$network" >/dev/null
docker run -d --name "$container" --network "$network" \
    --user 65532:65532 --cap-drop ALL --cap-add NET_ADMIN --cap-add NET_RAW \
    --security-opt no-new-privileges --entrypoint sleep "$image" infinity >/dev/null

# Match execNatRules: start an exec process in an already-running, non-root Pod.
docker exec -i "$container" capsh \
    --inh=cap_net_admin,cap_net_raw --addamb=cap_net_admin,cap_net_raw \
    --shell=/bin/bash -- -s -- --inside < "$0"

# Match the E2E rule probe without an intermediate non-root shell.
docker exec "$container" iptables -t nat -S PREROUTING
