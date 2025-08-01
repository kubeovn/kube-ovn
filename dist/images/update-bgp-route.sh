#!/usr/bin/env bash
# update-bgp-route.sh
# shellcheck disable=SC2086,SC2155

set -euo pipefail

GOBGP_BIN=${GOBGP_BIN:-$(command -v gobgp || true)}
[[ -z "$GOBGP_BIN" ]] && { echo "gobgp binary not found" >&2; exit 1; }

die() { echo "ERROR: $*" >&2; exit 1; }

external_iface="net1"
external_ipv4=$(ip addr show dev "${external_iface}" | grep 'inet ' | awk '{print $2}' | cut -d'/' -f1)
[[ -z "$external_ipv4" ]] && die "cannot determine external IPv4 address"

exec_cmd() {
  "$@" || die "failed: $*"
}

check_inited() {
  $GOBGP_BIN global rib &>/dev/null \
    || die "gobgp global RIB not initialized (did you 'gobgp global'?)"
}

add_announced_route() {
  check_inited
  echo "Adding routes..."
  for cidr in "$@"; do
    if [[ $cidr == *:* ]]; then
      family_flag="-a ipv6"
    else
      family_flag="-a ipv4"
    fi
    echo "  + Adding: $cidr"
    exec_cmd $GOBGP_BIN global rib $family_flag add \
             "$cidr" nexthop "$external_ipv4" origin igp
  done
  echo ""
}

del_announced_route() {
  check_inited
  echo "Deleting routes..."
  for cidr in "$@"; do
    if [[ $cidr == *:* ]]; then
      family_flag="-a ipv6"
    else
      family_flag="-a ipv4"
    fi
    echo "  - Deleting: $cidr"
    exec_cmd $GOBGP_BIN global rib $family_flag del \
             "$cidr" nexthop "$external_ipv4" origin igp
  done
  echo ""
}

flush_announced_route() {
  check_inited
  echo "Flushing all routes with next-hop $external_ipv4..."
  
  local routes_to_delete=()
  local found_routes=false
  
  # Get IPv4 routes with matching next-hop
  local ipv4_routes
  if ipv4_routes=$($GOBGP_BIN global rib -a ipv4 2>/dev/null | grep "$external_ipv4" | awk '{print $2}' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+/[0-9]+$'); then
    while IFS= read -r route; do
      if [[ -n "$route" ]]; then
        routes_to_delete+=("ipv4:$route")
        found_routes=true
      fi
    done <<< "$ipv4_routes"
  fi
  
  # Get IPv6 routes with matching next-hop
  local ipv6_routes
  if ipv6_routes=$($GOBGP_BIN global rib -a ipv6 2>/dev/null | grep "$external_ipv4" | awk '{print $2}' | grep -E '^[0-9a-fA-F:]+/[0-9]+$'); then
    while IFS= read -r route; do
      if [[ -n "$route" ]]; then
        routes_to_delete+=("ipv6:$route")
        found_routes=true
      fi
    done <<< "$ipv6_routes"
  fi
  
  if [[ "$found_routes" == false ]]; then
    echo "  No routes found with next-hop $external_ipv4"
    echo ""
    return 0
  fi
  
  # Delete all found routes
  for route_entry in "${routes_to_delete[@]}"; do
    local family="${route_entry%%:*}"
    local cidr="${route_entry#*:}"
    echo "  - Flushing: $cidr ($family)"
    exec_cmd $GOBGP_BIN global rib -a "$family" del \
             "$cidr" nexthop "$external_ipv4" origin igp
  done
  
  echo "Flushed ${#routes_to_delete[@]} routes with next-hop $external_ipv4"
  echo ""
}

list_announced_route() {
  check_inited
  
  echo "=== BGP Global RIB ==="
  echo "External Interface: $external_iface"
  echo "External IPv4: $external_ipv4"
  echo ""
  
  # Show IPv4 routes
  echo "--- IPv4 Routes ---"
  if $GOBGP_BIN global rib -a ipv4 2>/dev/null | grep -q "Network"; then
    $GOBGP_BIN global rib -a ipv4
  else
    echo "No IPv4 routes found"
  fi
  
  echo ""
  
  # Show IPv6 routes
  echo "--- IPv6 Routes ---"
  if $GOBGP_BIN global rib -a ipv6 2>/dev/null | grep -q "Network"; then
    $GOBGP_BIN global rib -a ipv6
  else
    echo "No IPv6 routes found"
  fi
  
  echo ""
  
  # Show routes that match our external IP as next-hop
  echo "--- Routes with Next-Hop $external_ipv4 ---"
  local found_matching=false
  
  # Check IPv4 routes with matching next-hop
  if $GOBGP_BIN global rib -a ipv4 2>/dev/null | awk -v nh="$external_ipv4" 'NR==1 || $3 == nh' | grep -v "Network" | grep -q .; then
    echo "IPv4 routes with next-hop $external_ipv4:"
    $GOBGP_BIN global rib -a ipv4 | awk -v nh="$external_ipv4" 'NR==1 || $3 == nh'
    found_matching=true
  fi
  
  # Check IPv6 routes with matching next-hop
  if $GOBGP_BIN global rib -a ipv6 2>/dev/null | awk -v nh="$external_ipv4" 'NR==1 || $3 == nh' | grep -v "Network" | grep -q .; then
    echo "IPv6 routes with next-hop $external_ipv4:"
    $GOBGP_BIN global rib -a ipv6 | awk -v nh="$external_ipv4" 'NR==1 || $3 == nh'
    found_matching=true
  fi
  
  if [[ "$found_matching" == false ]]; then
    echo "No routes found with next-hop $external_ipv4"
  fi
  
  echo ""
  echo "=========================================="
  echo ""
}

parse_sequential_args() {
  local operations=()
  local operation_args=()
  
  # Parse arguments and store operations in order
  for arg in "$@"; do
    case "$arg" in
      add_announced_route=*|add_announce_routes=*)
        operations+=("add")
        operation_args+=("${arg#*=}")
        ;;
      del_announced_route=*|del_announce_routes=*)
        operations+=("del")
        operation_args+=("${arg#*=}")
        ;;
      flush_announced_route)
        operations+=("flush")
        operation_args+=("")
        ;;
      list_announced_route)
        operations+=("list")
        operation_args+=("")
        ;;
      *)
        echo "Unknown argument: $arg" >&2
        usage
        ;;
    esac
  done
  
  # Execute operations in order
  for i in "${!operations[@]}"; do
    case "${operations[$i]}" in
      list)
        list_announced_route
        ;;
      flush)
        flush_announced_route
        ;;
      del)
        # Parse comma-separated CIDRs
        IFS=',' read -ra del_cidrs <<< "${operation_args[$i]}"
        # Remove leading and trailing spaces
        for j in "${!del_cidrs[@]}"; do
          del_cidrs[$j]=$(echo "${del_cidrs[$j]}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        done
        del_announced_route "${del_cidrs[@]}"
        ;;
      add)
        # Parse comma-separated CIDRs
        IFS=',' read -ra add_cidrs <<< "${operation_args[$i]}"
        # Remove leading and trailing spaces
        for j in "${!add_cidrs[@]}"; do
          add_cidrs[$j]=$(echo "${add_cidrs[$j]}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        done
        add_announced_route "${add_cidrs[@]}"
        ;;
    esac
  done
}

parse_key_value_args() {
  local add_routes=""
  local del_routes=""
  local list_routes=false
  local flush_routes=false
  
  # find key=value
  for arg in "$@"; do
    case "$arg" in
      add_announced_route=*|add_announce_routes=*)
        add_routes="${arg#*=}"  # extract after = values
        ;;
      del_announced_route=*|del_announce_routes=*)
        del_routes="${arg#*=}"
        ;;
      flush_announced_route)
        flush_routes=true
        ;;
      list_announced_route)
        list_routes=true
        ;;
      *)
        echo "Unknown argument: $arg" >&2
        usage
        ;;
    esac
  done

  if [[ "$flush_routes" == true ]]; then
    flush_announced_route
  fi

  if [[ "$list_routes" == true ]]; then
    list_announced_route
    return 0
  fi

  if [[ -n "$del_routes" ]]; then
    echo "Processing del_announced_route: $del_routes"
    # change cidrs to array
    IFS=',' read -ra del_cidrs <<< "$del_routes"
    # remove leading and trailing spaces
    for i in "${!del_cidrs[@]}"; do
      del_cidrs[$i]=$(echo "${del_cidrs[$i]}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    done
    del_announced_route "${del_cidrs[@]}"
  fi
  
  if [[ -n "$add_routes" ]]; then
    echo "Processing add_announced_route: $add_routes"
    # change cidrs to array
    IFS=',' read -ra add_cidrs <<< "$add_routes"
    # remove leading and trailing spaces from each CIDR
    for i in "${!add_cidrs[@]}"; do
      add_cidrs[$i]=$(echo "${add_cidrs[$i]}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    done
    add_announced_route "${add_cidrs[@]}"
  fi
}

usage() {
  cat >&2 <<EOF
Usage Options:
  1. Traditional: $0 <add_announced_route|del_announced_route|flush_announced_route|list_announced_route> <CIDR> [CIDR ...]
  2. Key-Value Arguments: $0 add_announced_route=CIDR1,CIDR2 [del_announced_route=CIDR3,CIDR4] [flush_announced_route] [list_announced_route]
  3. Sequential Processing: $0 list_announced_route del_announce_routes=CIDR1,CIDR2 add_announce_routes=CIDR3,CIDR4 flush_announced_route list_announced_route

Examples:
  $0 add_announced_route 10.100.0.0/24 192.168.1.0/24
  $0 del_announced_route 10.100.0.0/24 192.168.1.0/24
  $0 flush_announced_route
  $0 list_announced_route
  $0 add_announced_route=10.100.0.0/24,10.100.1.0/24 del_announced_route=10.0.0.0/24,10.0.1.0/24
  $0 flush_announced_route list_announced_route
  $0 list_announced_route flush_announced_route add_announce_routes=10.0.0.0/24,10.0.1.0/24 list_announced_route

Note: 
  - flush_announced_route removes ALL routes with next-hop $external_ipv4
  - Both 'del_announced_route=' and 'del_announce_routes=' are supported (same for add operations)
EOF
  exit 1
}

has_sequential_processing() {
  local has_list=false
  local has_operations=false
  
  for arg in "$@"; do
    case "$arg" in
      list_announced_route|flush_announced_route)
        has_list=true
        ;;
      add_announced_route=*|add_announce_routes=*|del_announced_route=*|del_announce_routes=*)
        has_operations=true
        ;;
    esac
  done
  
  # Return true if we have list/flush + operations (sequential processing)
  [[ "$has_list" == true && "$has_operations" == true ]]
}

has_key_value_args() {
  for arg in "$@"; do
    case "$arg" in
      *=*|list_announced_route|flush_announced_route)
        return 0  # key=value or list/flush command
        ;;
    esac
  done
  return 1  # key=value not found
}

# main entry point
main() {
  # if no arguments are provided, show usage
  [[ $# -eq 0 ]] && usage
  
  # check if we need sequential processing (list/flush + operations)
  if has_sequential_processing "$@"; then
    parse_sequential_args "$@"
    return 0
  fi
  
  # check if key=value arguments are used
  if has_key_value_args "$@"; then
    parse_key_value_args "$@"
    return 0
  fi
  
  [[ $# -lt 1 ]] && usage
  # TODO list announced routes network which nexthop is external_ipv4
  local op=$1; shift
  case "$op" in
    add_announced_route) add_announced_route "$@" ;;
    del_announced_route) del_announced_route "$@" ;;
    flush_announced_route) flush_announced_route ;;
    list_announced_route) list_announced_route ;;
    *) usage ;;
  esac
}

main "$@"