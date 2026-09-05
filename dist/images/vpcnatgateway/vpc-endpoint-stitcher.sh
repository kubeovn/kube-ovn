#!/usr/bin/env bash
# VpcEndpoint stitcher: in-pod iptables SNAT/DNAT between a VPC leg and a transit leg.
# Interfaces are persisted after init so subsequent rule updates survive restarts.

set -euo pipefail

ENV_FILE=/etc/kube-ovn/vpc-endpoint-stitcher.env
VPC_INTERFACE=${VPC_INTERFACE:-eth0}
TRANSIT_INTERFACE=${TRANSIT_INTERFACE:-net1}

iptables_cmd=$(command -v iptables)
if command -v iptables-legacy >/dev/null 2>&1; then
	# Prefer legacy when both stacks exist (common in Alpine/vpc-nat-gateway image).
	if iptables-legacy -t nat -S INPUT 1 >/dev/null 2>&1 || [ -n "$(iptables-legacy -t nat -L 2>/dev/null | head -1)" ]; then
		iptables_cmd=$(command -v iptables-legacy)
	fi
fi
if iptables-legacy -t nat -S INPUT 1 >/dev/null 2>&1; then
	iptables_cmd=$(command -v iptables-legacy)
fi

exec_cmd() {
	# shellcheck disable=SC2068
	$@
}

load_env() {
	if [[ -f "$ENV_FILE" ]]; then
		# shellcheck disable=SC1090
		source "$ENV_FILE"
	fi
}

save_env() {
	mkdir -p "$(dirname "$ENV_FILE")"
	cat >"$ENV_FILE" <<EOF
VPC_INTERFACE=${VPC_INTERFACE}
TRANSIT_INTERFACE=${TRANSIT_INTERFACE}
EOF
}

init() {
	local interfaces="${1:-}"
	if [[ -n "$interfaces" ]]; then
		VPC_INTERFACE=$(echo "$interfaces" | cut -d',' -f1 | tr -d ' ')
		TRANSIT_INTERFACE=$(echo "$interfaces" | cut -d',' -f2 | tr -d ' ')
	fi
	save_env
	exec_cmd sysctl -w net.ipv4.ip_forward=1
	exec_cmd sysctl -w net.ipv4.conf.all.rp_filter=0
	if ip link show "$VPC_INTERFACE" >/dev/null 2>&1; then
		sysctl -w "net.ipv4.conf.${VPC_INTERFACE}.rp_filter=0" >/dev/null
	else
		echo "warning: vpc interface $VPC_INTERFACE not present yet" >&2
	fi
	if ip link show "$TRANSIT_INTERFACE" >/dev/null 2>&1; then
		sysctl -w "net.ipv4.conf.${TRANSIT_INTERFACE}.rp_filter=0" >/dev/null
	else
		echo "warning: transit interface $TRANSIT_INTERFACE not present yet" >&2
	fi

	# Dedicated chains so reconcile can flush without touching unrelated NAT rules.
	$iptables_cmd -t nat -N VPC_EP_PREROUTING 2>/dev/null || true
	$iptables_cmd -t nat -N VPC_EP_POSTROUTING 2>/dev/null || true
	$iptables_cmd -t nat -C PREROUTING -j VPC_EP_PREROUTING 2>/dev/null ||
		exec_cmd $iptables_cmd -t nat -I PREROUTING 1 -j VPC_EP_PREROUTING
	$iptables_cmd -t nat -C POSTROUTING -j VPC_EP_POSTROUTING 2>/dev/null ||
		exec_cmd $iptables_cmd -t nat -I POSTROUTING 1 -j VPC_EP_POSTROUTING
	if ip link show "$VPC_INTERFACE" >/dev/null 2>&1 && ip link show "$TRANSIT_INTERFACE" >/dev/null 2>&1; then
		$iptables_cmd -C FORWARD -i "$VPC_INTERFACE" -o "$TRANSIT_INTERFACE" -j ACCEPT 2>/dev/null ||
			exec_cmd $iptables_cmd -I FORWARD 1 -i "$VPC_INTERFACE" -o "$TRANSIT_INTERFACE" -j ACCEPT
		$iptables_cmd -C FORWARD -i "$TRANSIT_INTERFACE" -o "$VPC_INTERFACE" -j ACCEPT 2>/dev/null ||
			exec_cmd $iptables_cmd -I FORWARD 1 -i "$TRANSIT_INTERFACE" -o "$VPC_INTERFACE" -j ACCEPT
	fi
	echo "vpc-endpoint stitcher initialized vpc=$VPC_INTERFACE transit=$TRANSIT_INTERFACE"
}

flush_rules() {
	load_env
	$iptables_cmd -t nat -F VPC_EP_PREROUTING 2>/dev/null || true
	$iptables_cmd -t nat -F VPC_EP_POSTROUTING 2>/dev/null || true
}

# consumer-sync args: localVIP,transitVIP,snatIP,proto:port,proto:port,...
consumer_sync() {
	load_env
	local local_vip=$1 transit_vip=$2 snat_ip=$3
	shift 3
	flush_rules
	local mapping
	for mapping in "$@"; do
		[[ -z "$mapping" ]] && continue
		local proto=${mapping%%:*}
		local port=${mapping#*:}
		exec_cmd $iptables_cmd -t nat -A VPC_EP_PREROUTING -d "$local_vip" -p "$proto" --dport "$port" \
			-j DNAT --to-destination "${transit_vip}:${port}"
	done
	exec_cmd $iptables_cmd -t nat -A VPC_EP_POSTROUTING -o "$TRANSIT_INTERFACE" -d "$transit_vip" \
		-j SNAT --to-source "$snat_ip"
	echo "consumer sync local=$local_vip transit=$transit_vip snat=$snat_ip"
}

# provider-sync args: transitVIP,proto:port:backendIP:backendPort,...
# Multiple backends for the same proto:port are load-balanced with iptables
# statistic nth (equal weight, connection-agnostic per-packet selection on new DNAT).
provider_sync() {
	load_env
	local transit_vip=$1
	shift 1
	flush_rules

	local -a group_keys=()
	local -A group_backends=()
	local mapping proto port backend key
	for mapping in "$@"; do
		[[ -z "$mapping" ]] && continue
		proto=${mapping%%:*}
		local rest=${mapping#*:}
		port=${rest%%:*}
		backend=${rest#*:}
		[[ -z "$proto" || -z "$port" || -z "$backend" ]] && continue
		key="${proto}:${port}"
		if [[ -z "${group_backends[$key]+x}" ]]; then
			group_keys+=("$key")
			group_backends[$key]="$backend"
		else
			group_backends[$key]+=" $backend"
		fi
	done

	local backends remaining dest
	for key in "${group_keys[@]}"; do
		proto=${key%%:*}
		port=${key#*:}
		# shellcheck disable=SC2206
		backends=(${group_backends[$key]})
		local n=${#backends[@]}
		local i=0
		for dest in "${backends[@]}"; do
			remaining=$((n - i))
			if [[ $remaining -gt 1 ]]; then
				exec_cmd $iptables_cmd -t nat -A VPC_EP_PREROUTING -d "$transit_vip" -p "$proto" --dport "$port" \
					-m statistic --mode nth --every "$remaining" --packet 0 \
					-j DNAT --to-destination "$dest"
			else
				exec_cmd $iptables_cmd -t nat -A VPC_EP_PREROUTING -d "$transit_vip" -p "$proto" --dport "$port" \
					-j DNAT --to-destination "$dest"
			fi
			i=$((i + 1))
		done
	done

	# Return path: backend sees stitcher VPC-leg IP as source.
	exec_cmd $iptables_cmd -t nat -A VPC_EP_POSTROUTING -o "$VPC_INTERFACE" -j MASQUERADE
	echo "provider sync transit=$transit_vip backends=${#group_keys[@]} ports"
}

case "${1:-}" in
init)
	init "${2:-}"
	;;
consumer-sync)
	shift
	consumer_sync "$@"
	;;
provider-sync)
	shift
	provider_sync "$@"
	;;
flush)
	flush_rules
	;;
*)
	echo "usage: $0 init [vpcIf,transitIf] | consumer-sync ... | provider-sync ... | flush" >&2
	exit 1
	;;
esac
