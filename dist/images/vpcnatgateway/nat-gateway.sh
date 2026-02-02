#!/usr/bin/env bash
# Network interfaces configuration
#
# ============================================================================
# IMPORTANT: tc classid Format Notes (READ BEFORE MODIFYING QoS CODE!)
# ============================================================================
#
# tc uses "major:minor" format for classid (e.g., "1:100" or "1:0x64").
# The minor part interpretation varies depending on context:
#
# 1. CREATION (tc class add / tc filter add):
#    - "1:0x64" -> minor = 100 (0x64 interpreted as hex)
#    - "1:64"   -> minor = 64  (64 interpreted as decimal)
#    - ALWAYS use "1:0x..." format to ensure hex interpretation
#
# 2. DISPLAY (tc filter show / tc class show):
#    - Outputs WITHOUT 0x prefix: "flowid 1:64" (this is hex, not decimal!)
#    - "flowid 1:64" means minor = 0x64 = 100 (decimal)
#
# 3. DELETION (tc class del / tc filter del):
#    - Must match the internal representation
#    - Use "1:0x64" to delete a class created with "1:0x64"
#
# COMMON BUG PATTERN:
#   - Create class with: tc class add ... classid 1:0x64   (minor = 100)
#   - tc shows: flowid 1:64 (hex display without prefix)
#   - Wrong deletion: tc class del ... classid 1:64        (minor = 64, WRONG!)
#   - Correct deletion: tc class del ... classid 1:0x64    (minor = 100, CORRECT!)
#
# RULE: When extracting classid from tc output for deletion, ALWAYS add "0x" prefix!
#       old_classid=$(... | sed 's/flowid 1:/0x/')  # Correct!
#       Then use: tc class del ... classid 1:$old_classid
#
# ============================================================================

# Read interfaces from persistent file
if [ -f /etc/kube-ovn/nat-gateway.env ]; then
    # shellcheck disable=SC1091
    source /etc/kube-ovn/nat-gateway.env
fi
# Default interfaces
VPC_INTERFACE=${VPC_INTERFACE:-"eth0"}
EXTERNAL_INTERFACE=${EXTERNAL_INTERFACE:-"net1"}
# Debug mode: set to "true" to enable verbose QoS debugging output
# In production, leave this as "false" to reduce log volume
QOS_DEBUG=${QOS_DEBUG:-"false"}

iptables_cmd=$(which iptables)
iptables_save_cmd=$(which iptables-save)
if iptables-legacy -t nat -S INPUT 1 2>/dev/null; then
    # use iptables-legacy for centos 7
    iptables_cmd=$(which iptables-legacy)
    iptables_save_cmd=$(which iptables-legacy-save)
fi

function show_help() {
    echo "NAT Gateway Configuration Script"
    echo ""
    echo "Environment Variables:"
    echo "  VPC_INTERFACE       - Interface connecting to VPC (default: eth0)"
    echo "  EXTERNAL_INTERFACE  - Interface connecting to external network (default: net1)"
    echo "  QOS_DEBUG           - Enable verbose QoS debugging output (default: false)"
    echo ""
    echo "Usage: $0 [COMMAND] [ARGS...]"
    echo ""
    echo "Commands:"
    echo "  init                     - Initialize iptables chains"
    echo "  subnet-route-add         - Add VPC internal routes"
    echo "  subnet-route-del         - Delete VPC internal routes"
    echo "  eip-add                  - Add external IP"
    echo "  eip-del                  - Delete external IP"
    echo "  floating-ip-add          - Add floating IP mapping"
    echo "  floating-ip-del          - Delete floating IP mapping"
    echo "  dnat-add                 - Add DNAT rule"
    echo "  dnat-del                 - Delete DNAT rule"
    echo "  snat-add                 - Add SNAT rule"
    echo "  snat-del                 - Delete SNAT rule"
    echo "  qos-add                  - Add QoS rule"
    echo "  qos-del                  - Delete QoS rule"
    echo "  eip-ingress-qos-add      - Add EIP ingress QoS"
    echo "  eip-egress-qos-add       - Add EIP egress QoS"
    echo "  eip-ingress-qos-del      - Delete EIP ingress QoS"
    echo "  eip-egress-qos-del       - Delete EIP egress QoS"
    echo "  get-iptables-version     - Show iptables version"
    echo ""
    echo "Examples:"
    echo "  # Use custom interfaces"
    echo "  $0 init net1, net2"
    echo ""
    echo "  # Use default interfaces (eth0,net1)"
    echo "  $0 init"
    echo ""
    echo "  # Environment variable override"
    echo "  VPC_INTERFACE=net1 EXTERNAL_INTERFACE=net2 $0 init"
}

function exec_cmd() {
    cmd=${@:1:${#}}
    $cmd
    ret=$?
    if [ $ret -ne 0 ]; then
        >&2 echo "failed to exec command \"$cmd\""
        exit $ret
    fi
}

function check_inited() {
    $iptables_save_cmd -t nat | grep SNAT_FILTER | grep SHARED_SNAT
    if [ $? -ne 0 ]; then
        >&2 echo "nat gateway not initialized"
        exit 1
    fi
}

function init() {
    interfaces=$1
    echo "init $interfaces"
    if [ -n "$interfaces" ]; then
        # First, remove all spaces around commas
        interfaces=$(echo "$interfaces" | sed 's/[[:space:]]*,[[:space:]]*/,/g')
        IFS=',' read -r -a interface_array <<< "$interfaces"
        if [ ${#interface_array[@]} -ne 2 ]; then
            >&2 echo "Error: Expected two interfaces separated by a comma (e.g., net1,net2)"
            exit 1
        fi

        # Trim any remaining leading and trailing whitespace
        VPC_INTERFACE=$(echo "${interface_array[0]}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
        EXTERNAL_INTERFACE=$(echo "${interface_array[1]}" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')

        # Validate interface names are not empty after trimming
        if [ -z "$VPC_INTERFACE" ] || [ -z "$EXTERNAL_INTERFACE" ]; then
            >&2 echo "Error: Interface names cannot be empty"
            exit 1
        fi
    fi
    echo "using VPC interface: $VPC_INTERFACE"
    echo "using External interface: $EXTERNAL_INTERFACE"
    # check if interfaces exist
    if ! ip link show "$VPC_INTERFACE" >/dev/null 2>&1; then
        >&2 echo "Error: VPC interface '$VPC_INTERFACE' not found"
        exit 1
    fi
    if ! ip link show "$EXTERNAL_INTERFACE" >/dev/null 2>&1; then
        >&2 echo "Error: External interface '$EXTERNAL_INTERFACE' not found"
        exit 1
    fi
    # Store interfaces persistently
    mkdir -p /etc/kube-ovn
    echo "VPC_INTERFACE=$VPC_INTERFACE" > /etc/kube-ovn/nat-gateway.env
    echo "EXTERNAL_INTERFACE=$EXTERNAL_INTERFACE" >> /etc/kube-ovn/nat-gateway.env

    # run once is enough
    $iptables_save_cmd | grep DNAT_FILTER && exit 0
    # add static chain
    # this also a flag to make sure init once
    $iptables_cmd -t nat -N DNAT_FILTER

    # add static chain
    $iptables_cmd -t nat -N SNAT_FILTER
    $iptables_cmd -t nat -N EXCLUSIVE_DNAT # floatingIp DNAT
    $iptables_cmd -t nat -N EXCLUSIVE_SNAT # floatingIp SNAT
    $iptables_cmd -t nat -N SHARED_DNAT
    $iptables_cmd -t nat -N SHARED_SNAT

    $iptables_cmd -t nat -A PREROUTING -j DNAT_FILTER
    $iptables_cmd -t nat -A DNAT_FILTER -j EXCLUSIVE_DNAT
    $iptables_cmd -t nat -A DNAT_FILTER -j SHARED_DNAT

    $iptables_cmd -t nat -A POSTROUTING -j SNAT_FILTER
    $iptables_cmd -t nat -A SNAT_FILTER -j EXCLUSIVE_SNAT
    $iptables_cmd -t nat -A SNAT_FILTER -j SHARED_SNAT

    # Load IFB kernel module for ingress QoS traffic shaping
    # IFB (Intermediate Functional Block) is required for ingress rate limiting using HTB
    # Load it early in init to detect any issues before QoS rules are applied
    echo "Loading IFB kernel module for ingress QoS support..."
    if ! modprobe ifb 2>/dev/null; then
        # Check if IFB module is already loaded
        if lsmod | grep -q "^ifb "; then
            echo "IFB kernel module already loaded"
        else
            echo "WARNING: Failed to load IFB kernel module. Ingress QoS may not work correctly."
            echo "         This is expected in some container environments. The system will attempt"
            echo "         to create IFB devices directly, which may work if the module is built-in."
        fi
    else
        echo "IFB kernel module loaded successfully"
    fi

    # Send gratuitous ARP for all the IPs on the external network interface at initialization
    # This is especially useful to update the MAC of the nexthop we announce to the BGP speaker
    # Only send ARP if there are IP addresses on the external interface (skip in no-IPAM mode)
    external_ips=$(ip -4 addr show dev $EXTERNAL_INTERFACE | awk '/inet /{print $2}' | cut -d/ -f1)
    if [ -n "$external_ips" ]; then
        echo "Sending gratuitous ARP for external IPs on $EXTERNAL_INTERFACE"
        echo "$external_ips" | xargs -n1 arping -I $EXTERNAL_INTERFACE -c 3 -U
        echo "Gratuitous ARP completed"
    else
        echo "INFO: No IP addresses on $EXTERNAL_INTERFACE, skipping gratuitous ARP (no-IPAM mode or waiting for EIP allocation)"
    fi
}


function get_iptables_version() {
  exec_cmd "$iptables_cmd --version"
}

function add_vpc_internal_route() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}
        nextHop=${arr[1]}

        exec_cmd "ip route replace $cidr via $nextHop dev $VPC_INTERFACE"
    done
}

function del_vpc_internal_route() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}

        exec_cmd "ip route del $cidr dev $VPC_INTERFACE"
    done
}

function del_vpc_external_route() {
    # make sure inited
    check_inited
    # never do this, if deleted, will cause error

    # for rule in $@
    # do
    #     arr=(${rule//,/ })
    #     cidr=${arr[0]}
    #     exec_cmd "ip route del $cidr dev $EXTERNAL_INTERFACE"
    #     sleep 1
    #     exec_cmd "ip route del default dev $EXTERNAL_INTERFACE"
    # done
}

function add_eip() {
    # make sure inited
   check_inited
    for rule in $@
    do
        eip=${rule}
        eip_without_prefix=(${eip//\// })
        exec_cmd "ip addr replace $eip dev $EXTERNAL_INTERFACE"
        exec_cmd "arping -I $EXTERNAL_INTERFACE -c 3 -U $eip_without_prefix"
    done

    if [ -n "$GATEWAY_V4" ]; then
        exec_cmd "ip route replace default via $GATEWAY_V4 dev $EXTERNAL_INTERFACE"
    fi

    if [ -n "$GATEWAY_V6" ]; then
        exec_cmd "ip -6 route replace default via $GATEWAY_V6 dev $EXTERNAL_INTERFACE"
    fi
}

function del_eip() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=${arr[0]}
        ipCidr=`ip addr show $EXTERNAL_INTERFACE | grep $eip | awk '{print $2 }'`
        if [ -n "$ipCidr" ]; then
            exec_cmd "ip addr del $ipCidr dev $EXTERNAL_INTERFACE"
        fi
    done
}

function add_floating_ip() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalIp=${arr[1]}
        # check if already exist
        $iptables_save_cmd | grep EXCLUSIVE_DNAT | grep -w "\-d $eip/32" | grep destination && exit 0
        exec_cmd "$iptables_cmd -t nat -A EXCLUSIVE_DNAT -d $eip -j DNAT --to-destination $internalIp"
        exec_cmd "$iptables_cmd -t nat -A EXCLUSIVE_SNAT -s $internalIp -j SNAT --to-source $eip"
    done
}

function del_floating_ip() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalIp=${arr[1]}
        # check if already exist
        $iptables_save_cmd  | grep EXCLUSIVE_DNAT | grep -w "\-d $eip/32" | grep destination
        if [ "$?" -eq 0 ];then
            exec_cmd "$iptables_cmd -t nat -D EXCLUSIVE_DNAT -d $eip -j DNAT --to-destination $internalIp"
            exec_cmd "$iptables_cmd -t nat -D EXCLUSIVE_SNAT -s $internalIp -j SNAT --to-source $eip"
            conntrack -D -d $eip 2>/dev/null || true
        fi
    done
}

function add_snat() {
    # make sure inited
    check_inited
    # iptables -t nat -F SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalCIDR=${arr[1]}
        randomFullyOption=${arr[2]}
        # check if already exist
        $iptables_save_cmd | grep SHARED_SNAT | grep "\-s $internalCIDR" | grep "source $eip" && exit 0
        exec_cmd "$iptables_cmd -t nat -A SHARED_SNAT -o $EXTERNAL_INTERFACE -s $internalCIDR -j SNAT --to-source $eip $randomFullyOption"
    done
}
function del_snat() {
    # make sure inited
    check_inited
    # iptables -t nat -F SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalCIDR=${arr[1]}
        # check if already exist
        ruleMatch=$($iptables_save_cmd | grep SHARED_SNAT | grep "\-s $internalCIDR" | grep "source $eip")
        if [ "$?" -eq 0 ];then
          ruleMatch=$(echo $ruleMatch | sed 's/-A //')
          exec_cmd "$iptables_cmd -t nat -D $ruleMatch"
        fi
    done
}


function add_dnat() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        dport=${arr[1]}
        protocol=${arr[2]}
        internalIp=${arr[3]}
        internalPort=${arr[4]}
        # check if already exist
        $iptables_save_cmd | grep SHARED_DNAT | grep -w "\-d $eip/32" | grep "p $protocol" | grep -w "dport $dport"| grep -w "destination $internalIp:$internalPort" && exit 0
        exec_cmd "$iptables_cmd -t nat -A SHARED_DNAT -p $protocol -d $eip --dport $dport -j DNAT --to-destination $internalIp:$internalPort"
    done
}


function del_dnat() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        dport=${arr[1]}
        protocol=${arr[2]}
        internalIp=${arr[3]}
        internalPort=${arr[4]}
        # check if already exist
        $iptables_save_cmd | grep SHARED_DNAT | grep -w "\-d $eip/32" | grep "p $protocol" | grep -w "dport $dport"| grep -w "destination $internalIp:$internalPort"
        if [ "$?" -eq 0 ];then
          exec_cmd "$iptables_cmd -t nat -D SHARED_DNAT -p $protocol -d $eip --dport $dport -j DNAT --to-destination $internalIp:$internalPort"
        fi
    done
}


# Escape dots for grep regex matching (e.g., "192.168.1.1/32" -> "192\.168\.1\.1/32")
function escape_for_regex() {
    echo "$1" | sed 's/\./\\./g'
}

# Convert burst value (in MB, may be decimal like 1.5) to bytes for tc command
# tc has issues parsing decimal values with unit suffixes (e.g., "1.5m" for cburst)
# By converting to bytes (integer), we avoid these parsing issues
# Args: burst_mb (value in megabytes, can be decimal)
# Returns: burst in bytes (integer)
function burst_mb_to_bytes() {
    local burst_mb=$1
    # Convert MB to bytes: multiply by 1048576 (1024*1024)
    # Use awk for floating point arithmetic
    local burst_bytes
    burst_bytes=$(awk "BEGIN {printf \"%.0f\", $burst_mb * 1048576}")
    echo "$burst_bytes"
}

# ============================================================================
# QoS Debugging and Verification Functions
# ============================================================================

# Log debug message only if QOS_DEBUG is enabled
# Args: message
function qos_debug() {
    [ "$QOS_DEBUG" = "true" ] && echo "DEBUG: $*" >&2
}

# Dump all tc QoS rules on a device for debugging
# Args: dev (device name), label (optional description)
# Only outputs when QOS_DEBUG=true
function dump_tc_qos_rules() {
    [ "$QOS_DEBUG" != "true" ] && return 0
    local dev=$1
    local label=${2:-""}
    echo "=== TC QoS Dump: $dev ${label:+($label)} ===" >&2
    echo "--- Qdisc ---" >&2
    tc qdisc show dev "$dev" 2>/dev/null || echo "(no qdisc)" >&2
    echo "--- Classes ---" >&2
    tc class show dev "$dev" 2>/dev/null || echo "(no classes)" >&2
    echo "--- Filters (with IP) ---" >&2
    tc -p filter show dev "$dev" parent 1: 2>/dev/null || echo "(no filters)" >&2
    echo "=== End of TC QoS Dump ===" >&2
}

# Verify a tc class exists with expected rate
# Args: dev, classid_hex (e.g., "0x4586"), expected_rate (e.g., "0.5" or "10")
# Returns: 0 if verified, 1 if failed
# Only outputs debug info when QOS_DEBUG=true
function verify_tc_class_exists() {
    local dev=$1
    local classid_hex=$2
    local expected_rate=$3

    local class_output
    class_output=$(tc class show dev "$dev" classid "1:$classid_hex" 2>/dev/null)

    if [ -z "$class_output" ]; then
        echo "ERROR: Class 1:$classid_hex not found on $dev" >&2
        return 1
    fi

    # Check if rate matches (tc shows rate in various formats)
    # tc may display rates as: "10Mbit", "0.5Mbit", "500Kbit", etc.
    # Convert expected_rate to Kbit for comparison: 0.5 Mbit = 500 Kbit
    local expected_kbit
    expected_kbit=$(awk "BEGIN {printf \"%.0f\", $expected_rate * 1000}")
    if echo "$class_output" | grep -qiE "rate ${expected_rate}[Mm]bit|rate ${expected_kbit}[Kk]bit"; then
        qos_debug "Verified class 1:$classid_hex exists with rate ~${expected_rate}Mbit on $dev"
        return 0
    else
        qos_debug "Class 1:$classid_hex exists but rate may not match expected ${expected_rate}Mbit"
        qos_debug "Actual class output: $class_output"
        return 0  # Still return success as class exists
    fi
}

# Verify a tc filter exists for a specific IP
# Args: dev, ip (e.g., "192.168.1.1"), match_direction (src/dst)
# Returns: 0 if found, 1 if not found
# Only outputs debug info when QOS_DEBUG=true
function verify_tc_filter_exists() {
    local dev=$1
    local ip=$2
    local match_direction=$3

    local ip_escaped
    ip_escaped=$(escape_for_regex "$ip/32")

    local filter_output
    filter_output=$(tc -p filter show dev "$dev" parent 1: 2>/dev/null)

    if echo "$filter_output" | grep -qiE "match ip $match_direction $ip_escaped"; then
        qos_debug "Verified filter for $ip ($match_direction) exists on $dev"
        return 0
    else
        echo "ERROR: Filter for $ip ($match_direction) NOT found on $dev" >&2
        return 1
    fi
}

# Verify a tc filter does NOT exist for a specific IP (after deletion)
# Args: dev, ip (e.g., "192.168.1.1"), match_direction (src/dst)
# Returns: 0 if NOT found (good), 1 if still exists (bad)
# Only outputs debug info when QOS_DEBUG=true
function verify_tc_filter_deleted() {
    local dev=$1
    local ip=$2
    local match_direction=$3

    local ip_escaped
    ip_escaped=$(escape_for_regex "$ip/32")

    local filter_output
    filter_output=$(tc -p filter show dev "$dev" parent 1: 2>/dev/null)

    if echo "$filter_output" | grep -qiE "match ip $match_direction $ip_escaped"; then
        echo "ERROR: Filter for $ip ($match_direction) still exists on $dev after deletion!" >&2
        return 1
    else
        qos_debug "Verified filter for $ip ($match_direction) was deleted from $dev"
        return 0
    fi
}

# ============================================================================
# End of QoS Debugging and Verification Functions
# ============================================================================

# Generate a unique classid from IP address for HTB class
# Uses all 4 octets with weighted sum to minimize collision probability
# Range: 0x1-0x7ffe (1-32766) - uses hex format for tc compatibility
# Note: Same IP will always produce same classid (deterministic)
# Collision probability is low for typical deployments (<100 EIPs)
#
# IMPORTANT: Returns classid WITH 0x prefix (e.g., "0x4586")
# - tc commands accept 0x prefix: tc class add ... classid 1:0x4586
# - tc filter show outputs WITHOUT prefix: flowid 1:4586
# - When grep'ing tc output, strip 0x prefix first: ${classid#0x}
function ip_to_classid() {
    local ip=$1
    # Validate IP format to prevent command injection in arithmetic expansion
    if ! [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "WARNING: Invalid IP format '$ip' passed to ip_to_classid. Using default classid 0x1." >&2
        echo "0x1"  # Default classid for invalid IP
        return
    fi
    IFS='.' read -r -a octets <<< "$ip"
    # Weighted hash using prime multipliers for better distribution
    # Formula ensures different IPs get different classids in most cases
    local hash=$(( (octets[0] * 251 + octets[1] * 241 + octets[2] * 239 + octets[3] * 233) % 32766 ))
    # classid range: 0x1-0x7fff (add 1 to avoid 0)
    printf "0x%x" $((hash + 1))
}

# Find an available classid, handling collision with different IP
# Args: dev, initial_classid (hex format), target_ip (the IP we want to assign this classid to), match_direction (src/dst)
# Returns: available classid in hex format (may be same as initial if no collision or collision with same IP)
function find_available_classid() {
    local dev=$1
    local classid_hex=$2
    local target_ip=$3
    local match_direction=$4

    # Convert hex to decimal for arithmetic
    local classid=$((classid_hex))
    # tc filter show outputs classid WITHOUT 0x prefix (e.g., "flowid 1:4586")
    # Strip 0x prefix for grep matching
    local classid_no_prefix=${classid_hex#0x}

    # Check if this classid is already in use
    local filter_output
    filter_output=$(tc -p filter show dev "$dev" parent 1: 2>/dev/null | grep -E "flowid 1:$classid_no_prefix\b" || true)

    if [ -n "$filter_output" ]; then
        # Classid is in use, check if it's for the same IP
        local target_ip_escaped
        target_ip_escaped=$(escape_for_regex "$target_ip/32")
        if echo "$filter_output" | grep -qiE "match ip $match_direction $target_ip_escaped"; then
            # Same IP, safe to reuse classid (this is an update scenario)
            printf "0x%x" $classid
            return
        fi

        # Collision with different IP - find alternative classid
        # Try offsets in a different range to avoid further collision
        local attempts=0
        while [ $attempts -lt 10 ]; do
            classid=$((classid + 3571))  # Use prime offset for better distribution
            if [ $classid -gt 32766 ]; then
                classid=$((classid - 32766))
            fi
            if [ $classid -lt 1 ]; then
                classid=1
            fi

            local new_classid_hex
            new_classid_hex=$(printf "0x%x" $classid)
            local existing
            existing=$(tc class show dev "$dev" classid 1:$new_classid_hex 2>/dev/null)
            if [ -z "$existing" ]; then
                # Found available classid
                printf "0x%x" $classid
                return
            fi
            attempts=$((attempts + 1))
        done

        # If all attempts failed, use the original classid anyway (very rare)
        # The old filter will be replaced
        echo "WARNING: find_available_classid failed to find available classid after 10 attempts for IP '$target_ip' on dev '$dev'. Using classid 0x$(printf '%x' $classid) which may cause collision." >&2
    fi

    printf "0x%x" $classid
}

# Delete existing HTB u32 filter and class
# Args: dev, ip_escaped (regex-escaped IP/32, e.g., "192\.168\.1\.1/32"), match_direction (src/dst)
function delete_htb_filter_and_class() {
    local dev=$1
    local ip_escaped=$2
    local match_direction=$3

    qos_debug "delete_htb_filter_and_class called: dev=$dev, ip_escaped=$ip_escaped, match_direction=$match_direction"

    local filter_output
    # -p: use human-readable IP format (192.168.1.1 instead of hex c0a80101)
    filter_output=$(tc -p filter show dev "$dev" parent 1: 2>/dev/null)

    qos_debug "filter_output length: ${#filter_output}"

    # Use -i for case-insensitive match (tc output may use "IP" or "ip" depending on version)
    if echo "$filter_output" | grep -qiE "match ip $match_direction $ip_escaped"; then
        qos_debug "Found matching filter for $ip_escaped on $dev"
        # Extract filter info: grep -iB2 gets 2 lines before the match line
        # tc output format is stable: flowid line is typically 1-2 lines before match line
        # Then we use a second grep to precisely extract the flowid line from context
        # This two-step approach is simple and reliable across tc versions
        local filter_info
        filter_info=$(echo "$filter_output" | grep -iB2 "match ip $match_direction $ip_escaped" | head -3)
        qos_debug "filter_info: $filter_info"
        # Only extract from the line containing 'flowid' (the actual filter line, not hash table declaration)
        local flowid_line
        flowid_line=$(echo "$filter_info" | grep "flowid" | head -1)
        qos_debug "flowid_line: $flowid_line"
        local old_handle old_prio old_classid
        old_handle=$(echo "$flowid_line" | grep -oE 'fh [0-9a-f:]+' | awk '{print $2}')
        old_prio=$(echo "$flowid_line" | grep -oE 'pref [0-9]+' | awk '{print $2}')
        # tc filter show outputs classid WITHOUT 0x prefix (e.g., "flowid 1:4586")
        # Add 0x prefix so tc class del interprets it as hex (tc accepts 0x prefix)
        old_classid=$(echo "$flowid_line" | grep -oE 'flowid 1:[0-9a-fA-F]+' | sed 's/flowid 1:/0x/')
        qos_debug "Extracted - old_handle=$old_handle, old_prio=$old_prio, old_classid=$old_classid"
        if [ -n "$old_handle" ] && [ -n "$old_prio" ]; then
            qos_debug "Deleting filter: tc filter del dev $dev parent 1: prio $old_prio handle $old_handle u32"
            tc filter del dev "$dev" parent 1: prio $old_prio handle $old_handle u32 2>/dev/null || true
        else
            qos_debug "Missing old_handle or old_prio, cannot delete filter"
        fi
        if [ -n "$old_classid" ]; then
            qos_debug "Deleting class: tc class del dev $dev classid 1:$old_classid"
            tc class del dev "$dev" classid 1:$old_classid 2>/dev/null || true
        else
            qos_debug "Missing old_classid, cannot delete class"
        fi

        # Verify deletion was successful
        # Extract IP from escaped pattern for verification
        local ip_for_verify
        ip_for_verify=$(echo "$ip_escaped" | sed 's/\\//g' | sed 's|/32||')
        if ! verify_tc_filter_deleted "$dev" "$ip_for_verify" "$match_direction"; then
            echo "WARNING: Filter deletion verification failed for $ip_for_verify on $dev" >&2
        fi
    else
        qos_debug "No matching filter found for pattern 'match ip $match_direction $ip_escaped' on $dev"
    fi
}

# Delete existing HTB matchall filter and class (egress with HTB qdisc)
# Args:
#   dev: network device name
#   target_classid: (optional) specific classid to delete, handles orphaned classes
#
# Why target_classid is needed:
#   When QoS policy is updated, the old class may become orphaned if:
#   1. Filter was deleted but class deletion failed (e.g., classid parsing failed)
#   2. Filter doesn't exist (deleted by another operation) so grep finds nothing
#   Without target_classid, orphaned classes cause "RTNETLINK answers: File exists"
#   error when adding new class with the same classid.
#
# Note: tc requires filter to be deleted BEFORE its associated class can be deleted.
#   This function first deletes the filter (if found), then deletes the class.
function delete_htb_matchall_filter_and_class() {
    local dev=$1
    local target_classid=${2:-}  # Optional: specific classid to delete (handles orphaned classes)

    local filter_output
    filter_output=$(tc filter show dev "$dev" parent 1: 2>/dev/null)

    if echo "$filter_output" | grep -qw "matchall"; then
        # Directly grep the matchall filter line that contains 'flowid'
        # This avoids the issue of grep -B2 picking up unrelated u32 filter lines
        local flowid_line
        flowid_line=$(echo "$filter_output" | grep "matchall.*flowid" | head -1)
        local old_handle old_prio old_classid
        # matchall filter uses "handle 0xNNNN" format, not "fh" like u32 filters
        old_handle=$(echo "$flowid_line" | grep -oE 'handle 0x[0-9a-fA-F]+' | sed 's/handle //')
        old_prio=$(echo "$flowid_line" | grep -oE 'pref [0-9]+' | awk '{print $2}')
        # tc filter show outputs classid WITHOUT 0x prefix (e.g., "flowid 1:ff00")
        # Add 0x prefix so tc class del interprets it as hex (tc accepts 0x prefix)
        old_classid=$(echo "$flowid_line" | grep -oE 'flowid 1:[0-9a-fA-F]+' | sed 's/flowid 1:/0x/')
        if [ -n "$old_handle" ] && [ -n "$old_prio" ]; then
            tc filter del dev "$dev" parent 1: prio "$old_prio" handle "$old_handle" matchall 2>/dev/null || true
        fi
        if [ -n "$old_classid" ]; then
            tc class del dev "$dev" classid 1:$old_classid 2>/dev/null || true
        fi
    fi

    # Also delete the target classid if provided (handles orphaned classes)
    # This ensures cleanup even if no filter exists pointing to this class
    if [ -n "$target_classid" ]; then
        tc class del dev "$dev" classid 1:$target_classid 2>/dev/null || true
    fi
}

# Delete HTB filter and class by classid (fallback when IP grep doesn't match)
# Args: dev, classid_hex (e.g., "0x4586")
function delete_htb_filter_by_classid() {
    local dev=$1
    local classid_hex=$2

    # tc filter show outputs classid WITHOUT 0x prefix (e.g., "flowid 1:4586")
    # Strip 0x prefix for grep matching
    local classid_no_prefix=${classid_hex#0x}
    local filter_by_classid
    filter_by_classid=$(tc filter show dev "$dev" parent 1: 2>/dev/null | grep -E "flowid 1:$classid_no_prefix\b" || true)
    if [ -n "$filter_by_classid" ]; then
        local old_prio old_handle
        old_prio=$(echo "$filter_by_classid" | grep -oE 'pref [0-9]+' | head -1 | awk '{print $2}')
        # u32 filters use "fh xxx:yyy" format, matchall filters use "handle 0xNNN" format
        old_handle=$(echo "$filter_by_classid" | grep -oE 'fh [0-9a-f:]+' | head -1 | awk '{print $2}')
        # Fallback: if fh format not found, try matchall handle format
        [ -z "$old_handle" ] && old_handle=$(echo "$filter_by_classid" | grep -oE 'handle 0x[0-9a-fA-F]+' | head -1 | sed 's/handle //')
        if [ -n "$old_prio" ] && [ -n "$old_handle" ]; then
            # Try both u32 and matchall since we don't know the filter type
            tc filter del dev "$dev" parent 1: prio "$old_prio" handle "$old_handle" u32 2>/dev/null || true
            tc filter del dev "$dev" parent 1: prio "$old_prio" handle "$old_handle" matchall 2>/dev/null || true
        fi
    fi

    # Delete class regardless of filter existence
    tc class del dev "$dev" classid 1:$classid_hex 2>/dev/null || true
}

# Get or create the IFB device name for a given interface
# IFB (Intermediate Functional Block) allows ingress traffic shaping using HTB
# by redirecting ingress traffic to the IFB device and applying egress shaping there
function get_ifb_device() {
    local dev=$1
    echo "ifb-${dev}"
}

# Setup IFB device for ingress traffic shaping
# This creates the IFB device and sets up the redirect from the physical interface
function setup_ifb_device() {
    local dev=$1
    local ifb_dev
    ifb_dev=$(get_ifb_device "$dev")

    # IFB module should already be loaded during init phase
    # Create IFB device if it doesn't exist
    if ! ip link show "$ifb_dev" >/dev/null 2>&1; then
        if ! ip link add "$ifb_dev" type ifb; then
            >&2 echo "failed to create IFB device $ifb_dev (is IFB kernel module loaded?)"
            exit 1
        fi
    fi

    # Set txqueuelen to 1000 (same as physical interface) to prevent queue overflow
    # Default qlen=32 is too small and causes packet drops during traffic bursts
    # This is critical for ingress QoS to work properly
    ip link set "$ifb_dev" txqueuelen 1000

    # Bring up the IFB device
    if ! ip link set "$ifb_dev" up; then
        >&2 echo "failed to bring up IFB device $ifb_dev"
        exit 1
    fi

    # Setup ingress qdisc on the physical interface to redirect traffic to IFB
    tc qdisc add dev "$dev" ingress 2>/dev/null || true

    # Setup HTB qdisc on IFB device for traffic shaping
    # Use default class 9999 for unclassified traffic (no rate limit)
    tc qdisc add dev "$ifb_dev" root handle 1: htb default 9999 2>/dev/null || true

    # Create default class 9999 for unclassified traffic (very high rate = no limit)
    # Without this class, unclassified traffic would be DROPPED because the default class doesn't exist
    # Use 10000mbit as "unlimited" rate (effectively no limit for normal network speeds)
    tc class add dev "$ifb_dev" parent 1: classid 1:9999 htb rate 10000mbit ceil 10000mbit 2>/dev/null || true

    # Add redirect action from physical interface ingress to IFB
    # Check if redirect filter already exists
    local existing_redirect
    existing_redirect=$(tc filter show dev "$dev" parent ffff: 2>/dev/null | grep -c "mirred" || true)
    if [ "$existing_redirect" -eq 0 ]; then
        # Redirect all ingress traffic to IFB device
        # Use lowest priority (65535, highest number = lowest priority) so it runs after all other filters
        if ! tc filter add dev "$dev" parent ffff: protocol all prio 65535 u32 match u32 0 0 action mirred egress redirect dev "$ifb_dev"; then
            >&2 echo "failed to add redirect filter from $dev to $ifb_dev"
            exit 1
        fi
    fi

    echo "$ifb_dev"
}

# Delete IFB filter and class for a specific IP
# Args: ifb_dev, ip_escaped (regex-escaped IP/32), match_direction (src/dst)
function delete_ifb_filter_and_class() {
    local ifb_dev=$1
    local ip_escaped=$2
    local match_direction=$3

    # Reuse the HTB deletion logic since IFB uses HTB
    delete_htb_filter_and_class "$ifb_dev" "$ip_escaped" "$match_direction"
}

# EIP-level ingress QoS using IFB + HTB (TCP-friendly, queues instead of drops)
# Caller: controller via execNatGwRules(gwPod, natGwEipIngressQoSAdd, rules)
# Flow: external --> $EXTERNAL_INTERFACE --> ingress redirect --> IFB --> HTB class --> internal
#
# Parameter count: 4 fields (comma-separated)
# Format: "ip,priority,rate,burst"
#
# Field definitions:
#   arr[0] ip       - pure IPv4 address without CIDR suffix (e.g., "192.168.1.1")
#   arr[1] priority - filter priority (lower = higher precedence)
#   arr[2] rate     - rate limit in Mbit/s (supports decimals, e.g., "1.5")
#   arr[3] burst    - burst size in MB (supports decimals, e.g., "1.5")
#
# Example: "172.21.0.23,2,25,25"
#
# Why IFB + HTB instead of police:
# - police drops packets exceeding the rate limit, which is very unfriendly to TCP
# - HTB queues packets and applies backpressure, allowing TCP to adapt smoothly
# - This results in actual throughput close to the configured rate limit
function eip_ingress_qos_add() {
    qos_debug "eip_ingress_qos_add called with args: $@"
    for rule in $@
    do
        arr=(${rule//,/ })
        local v4ip=${arr[0]}      # 172.21.0.23
        local priority=${arr[1]} # 2
        local rate=${arr[2]}     # Mbit/s
        local burst=${arr[3]}    # MB
        local dev="$EXTERNAL_INTERFACE"
        local matchDirection="dst"

        qos_debug "Processing ingress QoS rule - v4ip=$v4ip, priority=$priority, rate=$rate, burst=$burst, dev=$dev"

        # Setup IFB device and get its name
        local ifb_dev
        ifb_dev=$(setup_ifb_device "$dev")
        qos_debug "IFB device = $ifb_dev"

        # Delete any existing filter/class for this IP on IFB
        local v4ip_escaped
        v4ip_escaped=$(escape_for_regex "$v4ip/32")
        qos_debug "Calling delete_ifb_filter_and_class for ingress"
        delete_ifb_filter_and_class "$ifb_dev" "$v4ip_escaped" "$matchDirection"

        # Generate classid for this IP (reuse the same function as egress)
        local initial_classid
        initial_classid=$(ip_to_classid "$v4ip")
        local classid
        classid=$(find_available_classid "$ifb_dev" "$initial_classid" "$v4ip" "$matchDirection")
        qos_debug "classid for $v4ip = $classid (initial was $initial_classid)"

        # Delete any orphaned class with this classid
        tc class del dev "$ifb_dev" classid 1:$classid 2>/dev/null || true

        # Convert burst from MB to bytes (handles decimal values like 1.5)
        local burst_bytes
        burst_bytes=$(burst_mb_to_bytes "$burst")
        qos_debug "burst_bytes = $burst_bytes (from burst=$burst MB)"

        # Dump tc state BEFORE creating new rules (helps diagnose if old rules were deleted)
        dump_tc_qos_rules "$ifb_dev" "BEFORE creating ingress QoS for $v4ip"

        # Create HTB class with rate limiting on IFB device
        # rate: guaranteed bandwidth, ceil: maximum bandwidth (same for hard limit)
        # burst/cburst: use bytes to avoid tc parsing issues with decimal MB values
        qos_debug "Creating class: tc class add dev $ifb_dev parent 1: classid 1:$classid htb rate ${rate}mbit ceil ${rate}mbit burst ${burst_bytes} cburst ${burst_bytes}"
        exec_cmd "tc class add dev $ifb_dev parent 1: classid 1:$classid htb rate ${rate}mbit ceil ${rate}mbit burst ${burst_bytes} cburst ${burst_bytes}"

        # Add fq_codel as leaf qdisc for better handling of bursty traffic
        # fq_codel provides:
        # 1. Fair queuing - prevents single flow from monopolizing bandwidth
        # 2. Active Queue Management (AQM) - drops/marks packets before queue is full
        # 3. Reduced bufferbloat - maintains low latency under load
        # This is critical for ingress QoS where traffic arrives at line rate (e.g., 36 Gbps)
        # and needs to be rate-limited to much lower speeds (e.g., 1.5 Mbps)
        # Without fq_codel, the default pfifo_fast queue fills instantly, causing massive packet loss
        # and TCP congestion collapse
        # Use 'replace' instead of 'add' to make this idempotent (avoid "Exclusivity flag" error)
        exec_cmd "tc qdisc replace dev $ifb_dev parent 1:$classid fq_codel"

        # Create filter to classify traffic matching dst IP (ingress to this EIP) to this class
        qos_debug "Creating filter: tc filter add dev $ifb_dev parent 1: protocol ip prio $priority handle $classid u32 match ip $matchDirection $v4ip/32 flowid 1:$classid"
        exec_cmd "tc filter add dev $ifb_dev parent 1: protocol ip prio $priority handle $classid u32 match ip $matchDirection $v4ip/32 flowid 1:$classid"

        # Verify the rules were created correctly
        qos_debug "Verifying ingress QoS rules for $v4ip..."
        if ! verify_tc_class_exists "$ifb_dev" "$classid" "$rate"; then
            echo "ERROR: Failed to verify ingress class for $v4ip on $ifb_dev" >&2
        fi
        if ! verify_tc_filter_exists "$ifb_dev" "$v4ip" "$matchDirection"; then
            echo "ERROR: Failed to verify ingress filter for $v4ip on $ifb_dev" >&2
        fi

        # Dump tc state AFTER creating new rules
        dump_tc_qos_rules "$ifb_dev" "AFTER creating ingress QoS for $v4ip"
    done
}

# EIP-level egress QoS using HTB class (TCP-friendly, queues instead of drops)
# Caller: controller via execNatGwRules(gwPod, natGwEipEgressQoSAdd, rules)
#
# Parameter count: 4 fields (comma-separated)
# Format: "ip,priority,rate,burst"
#
# Field definitions:
#   arr[0] ip       - pure IPv4 address without CIDR suffix (e.g., "192.168.1.1")
#   arr[1] priority - filter priority (lower = higher precedence)
#   arr[2] rate     - rate limit in Mbit/s (supports decimals, e.g., "1.5")
#   arr[3] burst    - burst size in MB (supports decimals, e.g., "1.5")
#
# Example: "172.21.0.23,2,25,25"
#
# Flow: internal --> $EXTERNAL_INTERFACE --> HTB class --> external
function eip_egress_qos_add() {
    qos_debug "eip_egress_qos_add called with args: $@"
    for rule in $@
    do
        arr=(${rule//,/ })
        local v4ip=${arr[0]}      # 172.21.0.23
        local priority=${arr[1]} # 2
        local rate=${arr[2]}     # Mbit/s
        local burst=${arr[3]}    # MB
        local dev="$EXTERNAL_INTERFACE"

        qos_debug "Processing egress QoS rule - v4ip=$v4ip, priority=$priority, rate=$rate, burst=$burst, dev=$dev"

        # Create root HTB qdisc if not exists (default class 9999 for unclassified traffic)
        tc qdisc add dev $dev root handle 1: htb default 9999 2>/dev/null || true

        # Create default class 9999 for unclassified traffic (very high rate = no limit)
        # Without this class, unclassified traffic would be DROPPED because the default class doesn't exist
        # Use 10000mbit as "unlimited" rate (effectively no limit for normal network speeds)
        tc class add dev "$dev" parent 1: classid 1:9999 htb rate 10000mbit ceil 10000mbit 2>/dev/null || true

        # Delete any existing filter/class for this IP first (use IP/32 for CIDR match)
        local v4ip_escaped
        v4ip_escaped=$(escape_for_regex "$v4ip/32")
        qos_debug "Calling delete_htb_filter_and_class for egress"
        delete_htb_filter_and_class "$dev" "$v4ip_escaped" "src"

        # ip_to_classid returns classid WITH 0x prefix (e.g., "0x4586")
        # find_available_classid also returns WITH 0x prefix
        local initial_classid
        initial_classid=$(ip_to_classid "$v4ip")
        local classid
        classid=$(find_available_classid "$dev" "$initial_classid" "$v4ip" "src")
        qos_debug "classid for $v4ip = $classid (initial was $initial_classid)"

        # Delete any orphaned class with this classid (safe because find_available_classid
        # already verified it's either unused or belongs to this IP)
        tc class del dev $dev classid 1:$classid 2>/dev/null || true

        # Convert burst from MB to bytes (handles decimal values like 1.5)
        local burst_bytes
        burst_bytes=$(burst_mb_to_bytes "$burst")
        qos_debug "burst_bytes = $burst_bytes (from burst=$burst MB)"

        # Dump tc state BEFORE creating new rules (helps diagnose if old rules were deleted)
        dump_tc_qos_rules "$dev" "BEFORE creating egress QoS for $v4ip"

        # Create HTB class with rate limiting
        # rate: guaranteed bandwidth, ceil: maximum bandwidth (same for hard limit)
        # burst/cburst: use bytes to avoid tc parsing issues with decimal MB values
        qos_debug "Creating class: tc class add dev $dev parent 1: classid 1:$classid htb rate ${rate}mbit ceil ${rate}mbit burst ${burst_bytes} cburst ${burst_bytes}"
        exec_cmd "tc class add dev $dev parent 1: classid 1:$classid htb rate ${rate}mbit ceil ${rate}mbit burst ${burst_bytes} cburst ${burst_bytes}"

        # Add fq_codel as leaf qdisc for better handling of bursty traffic
        # This provides fair queuing and active queue management for egress traffic
        # Use 'replace' instead of 'add' to make this idempotent
        exec_cmd "tc qdisc replace dev $dev parent 1:$classid fq_codel"

        # Create filter to classify traffic matching src IP to this class
        # tc u32 match requires CIDR format, so append /32 for single IP
        exec_cmd "tc filter add dev $dev parent 1: protocol ip prio $priority handle $classid u32 match ip src $v4ip/32 flowid 1:$classid"

        # Verify the rules were created correctly
        qos_debug "Verifying egress QoS rules for $v4ip..."
        if ! verify_tc_class_exists "$dev" "$classid" "$rate"; then
            echo "ERROR: Failed to verify egress class for $v4ip on $dev" >&2
        fi
        if ! verify_tc_filter_exists "$dev" "$v4ip" "src"; then
            echo "ERROR: Failed to verify egress filter for $v4ip on $dev" >&2
        fi

        # Dump tc state AFTER creating new rules
        dump_tc_qos_rules "$dev" "AFTER creating egress QoS for $v4ip"
    done
}

# Generate a unique classid for qos_add egress HTB class
# Range: 0x8000-0xfffe (32768-65534) - reserved for NatGw-level QoS
# Uses hex format for tc compatibility and avoids collision with ip_to_classid (0x1-0x7fff)
#
# IMPORTANT: Returns classid WITH 0x prefix (e.g., "0x8000")
# - tc commands accept 0x prefix: tc class add ... classid 1:0x8000
# - tc filter show outputs WITHOUT prefix: flowid 1:8000
# - When grep'ing tc output, strip 0x prefix first: ${classid#0x}
function cidr_to_classid() {
    local cidr=$1
    local priority=$2
    # For matchall (empty cidr), use a fixed classid based on priority
    # Range: 0xff00-0xfffe (65280-65534, reserved for matchall rules)
    if [ -z "$cidr" ]; then
        local matchall_classid=$((65280 + (priority % 255)))
        printf "0x%x" $matchall_classid
        return
    fi
    local ip=${cidr%%/*}
    # Validate IP format to prevent command injection in arithmetic expansion
    if ! [[ "$ip" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "WARNING: Invalid IP format '$ip' in CIDR '$cidr' passed to cidr_to_classid. Using a default classid." >&2
        printf "0x%x" $((32768 + (priority % 255)))  # Default classid for invalid IP
        return
    fi
    IFS='.' read -r -a octets <<< "$ip"
    # Use weighted sum with different primes than ip_to_classid
    local hash=$(( (octets[0] * 11 + octets[1] * 13 + octets[2] * 17 + octets[3] * 19) % 32511 ))
    # Range: 0x8000-0xfeff (32768-65279, avoids collision with matchall range)
    local classid=$((hash + 32768))
    printf "0x%x" $classid
}

# Find an available classid for NatGw-level QoS, handling collision with different CIDR
# Args: dev, initial_classid (hex format), target_cidr, match_direction (src/dst)
# Returns: available classid in hex format (may be same as initial if no collision or collision with same CIDR)
# Note: This function handles the 0x8000-0xfeff range (NatGw QoS)
function find_available_classid_for_cidr() {
    local dev=$1
    local classid_hex=$2
    local target_cidr=$3
    local match_direction=$4

    # Convert hex to decimal for arithmetic
    local classid=$((classid_hex))
    # tc filter show outputs classid WITHOUT 0x prefix (e.g., "flowid 1:8000")
    # Strip 0x prefix for grep matching
    local classid_no_prefix=${classid_hex#0x}

    # Check if this classid is already in use
    local filter_output
    filter_output=$(tc -p filter show dev "$dev" parent 1: 2>/dev/null | grep -E "flowid 1:$classid_no_prefix\b" || true)

    if [ -n "$filter_output" ]; then
        # Classid is in use, check if it's for the same CIDR
        local target_cidr_escaped
        target_cidr_escaped=$(escape_for_regex "$target_cidr")
        if echo "$filter_output" | grep -qiE "match ip $match_direction $target_cidr_escaped"; then
            # Same CIDR, safe to reuse classid (this is an update scenario)
            printf "0x%x" $classid
            return
        fi

        # Collision with different CIDR - find alternative classid
        # Range: 0x8000-0xfeff (32768-65279, avoids matchall range 0xff00-0xfffe)
        local attempts=0
        while [ $attempts -lt 10 ]; do
            classid=$((classid + 3571))  # Use prime offset for better distribution
            if [ $classid -gt 65279 ]; then
                classid=$((classid - 32512))  # Wrap around within range
            fi
            if [ $classid -lt 32768 ]; then
                classid=32768
            fi

            local new_classid_hex
            new_classid_hex=$(printf "0x%x" $classid)
            local existing
            existing=$(tc class show dev "$dev" classid 1:$new_classid_hex 2>/dev/null)
            if [ -z "$existing" ]; then
                # Found available classid
                printf "0x%x" $classid
                return
            fi
            attempts=$((attempts + 1))
        done

        # If all attempts failed, use the original classid anyway (very rare)
        # The old filter will be replaced
        echo "WARNING: find_available_classid_for_cidr failed to find available classid after 10 attempts for CIDR '$target_cidr' on dev '$dev'. Using classid 0x$(printf '%x' $classid) which may cause collision." >&2
    fi

    printf "0x%x" $classid
}

# NatGw-level QoS for interface or IP range
# Caller: controller via execNatGwRules(gwPod, QoSAdd, rules)
#
# Parameter count: 9 fields (comma-separated)
# Format: "direction,dev,priority,classifierType,matchType,matchDirection,cidr,rate,burst"
#
# Field definitions:
#   arr[0] direction       - "ingress" or "egress"
#   arr[1] dev             - network device name (e.g., "net1")
#   arr[2] priority        - filter priority (lower = higher precedence), also used for classid calculation
#   arr[3] classifierType  - "u32" (match specific IP) or "matchall" (all traffic on device)
#   arr[4] matchType       - always "ip" for u32, empty for matchall
#   arr[5] matchDirection  - "src" or "dst" for u32, empty for matchall
#   arr[6] cidr            - IP with /32 suffix (e.g., "192.168.1.1/32") for u32, empty for matchall
#   arr[7] rate            - rate limit in Mbit/s (supports decimals, e.g., "1.5")
#   arr[8] burst           - burst size in MB (supports decimals, e.g., "1.5")
#
# Examples:
#   u32 filter:     "egress,net1,2,u32,ip,dst,192.168.1.1/32,25,25"
#   matchall filter: "egress,net1,3,matchall,,,,30,30"
function qos_add() {
    for rule in $@
    do
        IFS=',' read -r -a arr <<< "$rule"
        local qdiscType=${arr[0]}        # ingress|egress
        local dev=${arr[1]}              # net1
        local priority=${arr[2]}         # 2, 3, etc.
        local classifierType=${arr[3]}   # u32|matchall
        local matchType=${arr[4]}        # ip (or empty for matchall)
        local matchDirection=${arr[5]}   # src|dst (or empty for matchall)
        local cidr=${arr[6]}             # 192.168.1.1/32 (or empty for matchall)
        local rate=${arr[7]}             # Mbit/s
        local burst=${arr[8]}            # MB

        if [ "$qdiscType" == "ingress" ]; then
            # Ingress: use IFB + HTB for TCP-friendly traffic shaping
            local ifb_dev
            ifb_dev=$(setup_ifb_device "$dev")

            # cidr_to_classid returns classid WITH 0x prefix (e.g., "0x8000")
            local initial_classid=$(cidr_to_classid "$cidr" "$priority")
            local classid

            # Delete existing rule for this IP/matchall before adding new one
            if [ "$classifierType" == "u32" ]; then
                local cidr_escaped
                cidr_escaped=$(escape_for_regex "$cidr")
                delete_htb_filter_and_class "$ifb_dev" "$cidr_escaped" "$matchDirection"

                # Check for collision and find available classid
                classid=$(find_available_classid_for_cidr "$ifb_dev" "$initial_classid" "$cidr" "$matchDirection")
            elif [ "$classifierType" == "matchall" ]; then
                # Pass initial_classid to ensure orphaned classes are cleaned up
                delete_htb_matchall_filter_and_class "$ifb_dev" "$initial_classid"
                # matchall uses fixed classid, no collision detection needed
                classid=$initial_classid
            fi

            # Delete any orphaned class with this classid
            tc class del dev "$ifb_dev" classid 1:$classid 2>/dev/null || true

            # Convert burst from MB to bytes (handles decimal values like 1.5)
            local burst_bytes
            burst_bytes=$(burst_mb_to_bytes "$burst")

            # Create HTB class on IFB
            exec_cmd "tc class add dev $ifb_dev parent 1: classid 1:$classid htb rate ${rate}mbit ceil ${rate}mbit burst ${burst_bytes} cburst ${burst_bytes}"

            # Add fq_codel as leaf qdisc for better handling of bursty traffic
            # Use 'replace' instead of 'add' to make this idempotent
            exec_cmd "tc qdisc replace dev $ifb_dev parent 1:$classid fq_codel"

            # Create filter on IFB
            if [ "$classifierType" == "u32" ]; then
                exec_cmd "tc filter add dev $ifb_dev parent 1: protocol ip prio $priority handle $classid u32 match $matchType $matchDirection $cidr flowid 1:$classid"
            elif [ "$classifierType" == "matchall" ]; then
                exec_cmd "tc filter add dev $ifb_dev parent 1: protocol ip prio $priority handle $classid matchall flowid 1:$classid"
            fi

        elif [ "$qdiscType" == "egress" ]; then
            # Egress: use HTB class (queue packets instead of dropping)
            tc qdisc add dev $dev root handle 1: htb default 9999 2>/dev/null || true

            # Create default class 9999 for unclassified traffic (very high rate = no limit)
            # Without this class, unclassified traffic would be DROPPED because the default class doesn't exist
            tc class add dev "$dev" parent 1: classid 1:9999 htb rate 10000mbit ceil 10000mbit 2>/dev/null || true

            # cidr_to_classid returns classid WITH 0x prefix (e.g., "0x8000")
            local initial_classid=$(cidr_to_classid "$cidr" "$priority")
            local classid

            # Delete existing rule for this IP/matchall before adding new one
            if [ "$classifierType" == "u32" ]; then
                local cidr_escaped
                cidr_escaped=$(escape_for_regex "$cidr")
                delete_htb_filter_and_class "$dev" "$cidr_escaped" "$matchDirection"

                # Check for collision and find available classid
                classid=$(find_available_classid_for_cidr "$dev" "$initial_classid" "$cidr" "$matchDirection")
            elif [ "$classifierType" == "matchall" ]; then
                # Pass initial_classid to ensure orphaned classes are cleaned up
                delete_htb_matchall_filter_and_class "$dev" "$initial_classid"
                # matchall uses fixed classid, no collision detection needed
                classid=$initial_classid
            fi

            # Delete any orphaned class with this classid (must be after filter deletion)
            tc class del dev "$dev" classid 1:$classid 2>/dev/null || true

            # Convert burst from MB to bytes (handles decimal values like 1.5)
            local burst_bytes
            burst_bytes=$(burst_mb_to_bytes "$burst")

            # Create HTB class
            exec_cmd "tc class add dev $dev parent 1: classid 1:$classid htb rate ${rate}mbit ceil ${rate}mbit burst ${burst_bytes} cburst ${burst_bytes}"

            # Add fq_codel as leaf qdisc for better handling of bursty traffic
            # Use 'replace' instead of 'add' to make this idempotent
            exec_cmd "tc qdisc replace dev $dev parent 1:$classid fq_codel"

            # Create filter
            if [ "$classifierType" == "u32" ]; then
                exec_cmd "tc filter add dev $dev parent 1: protocol ip prio $priority handle $classid u32 match $matchType $matchDirection $cidr flowid 1:$classid"
            elif [ "$classifierType" == "matchall" ]; then
                exec_cmd "tc filter add dev $dev parent 1: protocol ip prio $priority handle $classid matchall flowid 1:$classid"
            fi
        fi
    done
}

# Delete NatGw-level QoS rule
# Caller: controller via execNatGwRules(gwPod, QoSDel, rules)
#
# Parameter count: 7 fields (comma-separated)
# Format: "direction,dev,priority,classifierType,matchType,matchDirection,cidr"
#
# Field definitions:
#   arr[0] direction       - "ingress" or "egress"
#   arr[1] dev             - network device name (e.g., "net1")
#   arr[2] priority        - filter priority, used for classid calculation
#   arr[3] classifierType  - "u32" (match specific IP) or "matchall" (all traffic on device)
#   arr[4] matchType       - always "ip" for u32, empty for matchall (unused but kept for API compatibility)
#   arr[5] matchDirection  - "src" or "dst" for u32, empty for matchall
#   arr[6] cidr            - IP with /32 suffix (e.g., "192.168.1.1/32") for u32, empty for matchall
#
# Note: rate and burst are NOT needed for deletion (only 7 fields vs 9 for add)
#
# Examples:
#   u32 filter:     "egress,net1,2,u32,ip,dst,192.168.1.1/32"
#   matchall filter: "egress,net1,3,matchall,,,"
function qos_del() {
    for rule in $@
    do
        IFS=',' read -r -a arr <<< "$rule"
        local qdiscType=${arr[0]}        # ingress|egress
        local dev=${arr[1]}              # net1
        local priority=${arr[2]}         # 2, 3, etc.
        local classifierType=${arr[3]}   # u32|matchall
        # arr[4] matchType - always "ip" for u32, unused but kept for API compatibility
        local matchDirection=${arr[5]}   # src|dst (or empty for matchall)
        local cidr=${arr[6]}             # 192.168.1.1/32 (or empty for matchall)

        if [ "$qdiscType" == "ingress" ]; then
            # Ingress rules are on IFB device
            local ifb_dev
            ifb_dev=$(get_ifb_device "$dev")

            # Check if IFB device exists and has HTB qdisc
            if ! ip link show "$ifb_dev" >/dev/null 2>&1; then
                continue
            fi
            if ! tc qdisc show dev "$ifb_dev" | grep -q "htb 1:"; then
                continue
            fi

            # cidr_to_classid returns classid WITH 0x prefix (e.g., "0x8000")
            local classid=$(cidr_to_classid "$cidr" "$priority")

            # For u32 filter, find and delete by IP match
            if [ "$classifierType" == "u32" ]; then
                local cidr_escaped
                cidr_escaped=$(escape_for_regex "$cidr")
                delete_htb_filter_and_class "$ifb_dev" "$cidr_escaped" "$matchDirection"
            elif [ "$classifierType" == "matchall" ]; then
                delete_htb_matchall_filter_and_class "$ifb_dev" "$classid"
            fi

            # Also try to delete filter by classid (handles case where grep pattern didn't match)
            delete_htb_filter_by_classid "$ifb_dev" "$classid"

        elif [ "$qdiscType" == "egress" ]; then
            # Ensure HTB root qdisc exists
            if ! tc qdisc show dev "$dev" | grep -q "htb 1:"; then
                continue
            fi

            # cidr_to_classid returns classid WITH 0x prefix (e.g., "0x8000")
            local classid=$(cidr_to_classid "$cidr" "$priority")

            # For u32 filter, find and delete by IP match (handles collision case)
            if [ "$classifierType" == "u32" ]; then
                local cidr_escaped
                cidr_escaped=$(escape_for_regex "$cidr")
                delete_htb_filter_and_class "$dev" "$cidr_escaped" "$matchDirection"
            elif [ "$classifierType" == "matchall" ]; then
                delete_htb_matchall_filter_and_class "$dev" "$classid"
            fi

            # Also try to delete filter by classid (handles case where grep pattern didn't match)
            delete_htb_filter_by_classid "$dev" "$classid"
        fi
    done
}

# Delete EIP-level ingress QoS rule
# Caller: controller via execNatGwRules(gwPod, natGwEipIngressQoSDel, rules)
#
# Parameter count: 1 field
# Format: "ip"
#
# Field definitions:
#   arr[0] ip - pure IPv4 address without CIDR suffix (e.g., "192.168.1.1")
#
# Example: "172.21.0.23"
#
# Note: Only IP is needed for deletion (rate/burst/priority not required)
function eip_ingress_qos_del() {
    for rule in $@
    do
        arr=(${rule//,/ })
        local v4ip=${arr[0]}  # 172.21.0.23
        local matchDirection="dst"
        local dev="$EXTERNAL_INTERFACE"
        local ifb_dev
        ifb_dev=$(get_ifb_device "$dev")

        # Check if IFB device exists and has HTB qdisc
        if ! ip link show "$ifb_dev" >/dev/null 2>&1; then
            continue
        fi
        if ! tc qdisc show dev "$ifb_dev" | grep -q "htb 1:"; then
            continue
        fi

        # ip_to_classid returns classid WITH 0x prefix (e.g., "0x4586")
        local classid
        classid=$(ip_to_classid "$v4ip")

        # Use helper function to delete HTB filter and class by IP match on IFB
        local v4ip_escaped
        v4ip_escaped=$(escape_for_regex "$v4ip/32")
        delete_ifb_filter_and_class "$ifb_dev" "$v4ip_escaped" "$matchDirection"

        # Also try to delete filter by classid (handles case where grep pattern didn't match)
        delete_htb_filter_by_classid "$ifb_dev" "$classid"
    done
}

# Delete EIP-level egress QoS rule
# Caller: controller via execNatGwRules(gwPod, natGwEipEgressQoSDel, rules)
#
# Parameter count: 1 field
# Format: "ip"
#
# Field definitions:
#   arr[0] ip - pure IPv4 address without CIDR suffix (e.g., "192.168.1.1")
#
# Example: "172.21.0.23"
#
# Note: Only IP is needed for deletion (rate/burst/priority not required)
function eip_egress_qos_del() {
    for rule in $@
    do
        arr=(${rule//,/ })
        local v4ip=${arr[0]}  # 172.21.0.23
        local dev="$EXTERNAL_INTERFACE"

        # Ensure HTB root qdisc exists before trying to delete
        if ! tc qdisc show dev "$dev" | grep -q "htb 1:"; then
            continue
        fi

        # ip_to_classid returns classid WITH 0x prefix (e.g., "0x4586")
        local classid
        classid=$(ip_to_classid "$v4ip")

        # Use helper function to delete HTB filter and class by IP match
        local v4ip_escaped
        v4ip_escaped=$(escape_for_regex "$v4ip/32")
        delete_htb_filter_and_class "$dev" "$v4ip_escaped" "src"

        # Also try to delete filter by classid (handles case where grep pattern didn't match)
        delete_htb_filter_by_classid "$dev" "$classid"
    done
}


rules=${@:2:${#}}
opt=$1
case $opt in
    init)
        # get user interfaces if provided from input
        interfaces="$rules"
        init "$interfaces"
        ;;
    subnet-route-add)
        echo "subnet-route-add $rules"
        add_vpc_internal_route $rules
        ;;
    subnet-route-del)
        echo "subnet-route-del $rules"
        del_vpc_internal_route $rules
        ;;
    eip-add)
        echo "eip-add $rules"
        add_eip $rules
        ;;
    eip-del)
        echo "eip-del $rules"
        del_eip $rules
        ;;
    dnat-add)
        echo "dnat-add $rules"
        add_dnat $rules
        ;;
    dnat-del)
        echo "dnat-del $rules"
        del_dnat $rules
        ;;
    snat-add)
        echo "snat-add $rules"
        add_snat $rules
        ;;
    snat-del)
        echo "snat-del $rules"
        del_snat $rules
        ;;
    floating-ip-add)
        echo "floating-ip-add $rules"
        add_floating_ip $rules
        ;;
    floating-ip-del)
        echo "floating-ip-del $rules"
        del_floating_ip $rules
        ;;
    get-iptables-version)
        echo "get-iptables-version $rules"
        get_iptables_version $rules
        ;;
    help|--help|-h)
        show_help
        ;;
    eip-ingress-qos-add)
        echo "eip-ingress-qos-add $rules"
        eip_ingress_qos_add $rules
        ;;
    eip-egress-qos-add)
        echo "eip-egress-qos-add $rules"
        eip_egress_qos_add $rules
        ;;
    eip-ingress-qos-del)
        echo "eip-ingress-qos-del $rules"
        eip_ingress_qos_del $rules
        ;;
    eip-egress-qos-del)
        echo "eip-egress-qos-del $rules"
        eip_egress_qos_del $rules
        ;;
    qos-add)
        echo "qos-add $rules"
        qos_add $rules
        ;;
    qos-del)
        echo "qos-del $rules"
        qos_del $rules
        ;;
    *)
        echo "Unknown command: $opt"
        echo ""
        show_help
        exit 1
        ;;
esac
