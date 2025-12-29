#!/usr/bin/env bash
# Network interfaces configuration

# Read interfaces from persistent file
if [ -f /etc/kube-ovn/nat-gateway.env ]; then
    # shellcheck disable=SC1091
    source /etc/kube-ovn/nat-gateway.env
fi
# Default interfaces
VPC_INTERFACE=${VPC_INTERFACE:-"eth0"}
EXTERNAL_INTERFACE=${EXTERNAL_INTERFACE:-"net1"}

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
    echo "  hairpin-snat-add         - Add hairpin SNAT rule for internal FIP access"
    echo "  hairpin-snat-del         - Delete hairpin SNAT rule"
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
    $iptables_cmd -t nat -N HAIRPIN_SNAT   # hairpin SNAT for internal FIP access
    $iptables_cmd -t nat -N SHARED_DNAT
    $iptables_cmd -t nat -N SHARED_SNAT

    $iptables_cmd -t nat -A PREROUTING -j DNAT_FILTER
    $iptables_cmd -t nat -A DNAT_FILTER -j EXCLUSIVE_DNAT
    $iptables_cmd -t nat -A DNAT_FILTER -j SHARED_DNAT

    $iptables_cmd -t nat -A POSTROUTING -j SNAT_FILTER
    $iptables_cmd -t nat -A SNAT_FILTER -j EXCLUSIVE_SNAT
    $iptables_cmd -t nat -A SNAT_FILTER -j SHARED_SNAT
    $iptables_cmd -t nat -A SNAT_FILTER -j HAIRPIN_SNAT

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

# Check if the given CIDR exists in VPC_INTERFACE's routes (indicates it's an internal CIDR)
# This is used to determine if hairpin SNAT is needed for a given SNAT rule
# Args: $1 - CIDR to check (e.g., "10.0.1.0/24")
# Returns: 0 if the CIDR is found in VPC_INTERFACE routes, 1 otherwise
# Example: VPC_INTERFACE=eth0, route "10.0.1.0/24 dev eth0" exists
#          is_internal_cidr "10.0.1.0/24" -> returns 0 (true)
#          is_internal_cidr "192.168.1.0/24" -> returns 1 (false, not in routes)
function is_internal_cidr() {
    local cidr="$1"
    if [ -z "$cidr" ]; then
        return 1
    fi
    # Match CIDR at the start of line to ensure exact match
    # e.g., "10.0.1.0/24 dev eth0" matches, but "10.0.1.0/25 ..." does not
    if ip -4 route show dev "$VPC_INTERFACE" | grep -q "^$cidr "; then
        return 0
    fi
    return 1
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
            conntrack -D -d $eip 2>/dev/nul || true
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
        # check if already exist, skip adding if exists (idempotent)
        if ! $iptables_save_cmd | grep SHARED_SNAT | grep -w -- "-s $internalCIDR" | grep -E -- "--to-source $eip(\$| )" > /dev/null; then
            exec_cmd "$iptables_cmd -t nat -A SHARED_SNAT -o $EXTERNAL_INTERFACE -s $internalCIDR -j SNAT --to-source $eip $randomFullyOption"
        fi
        # Add hairpin SNAT when internalCIDR is routed via VPC_INTERFACE
        # This enables internal VMs to access other internal VMs via FIP
        if is_internal_cidr "$internalCIDR"; then
            echo "SNAT cidr $internalCIDR is internal, adding hairpin SNAT with EIP $eip"
            add_hairpin_snat "$eip,$internalCIDR"
        fi
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
        ruleMatch=$($iptables_save_cmd | grep SHARED_SNAT | grep -w -- "-s $internalCIDR" | grep -E -- "--to-source $eip(\$| )")
        if [ "$?" -eq 0 ];then
          ruleMatch=$(echo $ruleMatch | sed 's/-A //')
          exec_cmd "$iptables_cmd -t nat -D $ruleMatch"
        fi
        # Delete hairpin SNAT when internalCIDR is routed via VPC_INTERFACE
        if is_internal_cidr "$internalCIDR"; then
            echo "SNAT cidr $internalCIDR is internal, deleting hairpin SNAT with EIP $eip"
            del_hairpin_snat "$eip,$internalCIDR"
        fi
    done
}

# Hairpin SNAT: Enables internal VM to access another internal VM's FIP
# Packet flow when VM A accesses VM B's EIP:
# 1. VM A (10.0.1.6) -> EIP (10.1.69.216) arrives at NAT GW
# 2. DNAT translates destination to VM B's internal IP (10.0.1.11)
# 3. Without hairpin SNAT, reply from VM B goes directly to VM A (same subnet),
#    but VM A expects reply from EIP, causing asymmetric routing failure
# 4. Hairpin SNAT translates source to EIP, ensuring symmetric return path via NAT GW
#
# RECOMMENDED: NAT-GW binds to a single VPC internal subnet. In this case,
# only one hairpin SNAT rule is needed (matching the VPC's directly connected route).
#
# Multi-subnet scenarios are supported but NOT recommended. For multiple subnets,
# create separate NAT gateways for each subnet to achieve more direct forwarding paths.
# Each CIDR can only have one hairpin rule to avoid conflicting SNAT sources.
#
# Rule format: eip,internalCIDR
# Example: 10.1.69.219,10.0.1.0/24
# Creates: iptables -t nat -A HAIRPIN_SNAT -s 10.0.1.0/24 -d 10.0.1.0/24 -j SNAT --to-source 10.1.69.219
function add_hairpin_snat() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalCIDR=${arr[1]}

        # Cache iptables-save output to avoid redundant calls (performance optimization)
        local existing_rules
        existing_rules=$($iptables_save_cmd -t nat | grep HAIRPIN_SNAT | grep -w -- "-s $internalCIDR" | grep -w -- "-d $internalCIDR")

        # Check if this exact rule already exists (idempotent)
        if echo "$existing_rules" | grep -qE -- "--to-source $eip(\$| )"; then
            echo "Hairpin SNAT rule for $internalCIDR with EIP $eip already exists, skipping"
            continue
        fi

        # Check if this CIDR already has a hairpin rule with a different EIP
        if [ -n "$existing_rules" ]; then
            echo "WARNING: Hairpin SNAT rule for $internalCIDR already exists with different EIP. Skipping."
            continue
        fi

        exec_cmd "$iptables_cmd -t nat -A HAIRPIN_SNAT -s $internalCIDR -d $internalCIDR -j SNAT --to-source $eip"
        echo "Hairpin SNAT rule added: $internalCIDR -> $eip"
    done
}

function del_hairpin_snat() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalCIDR=${arr[1]}
        # check if rule exists (idempotent - skip if not found)
        if $iptables_save_cmd -t nat | grep HAIRPIN_SNAT | grep -w -- "-s $internalCIDR" | grep -w -- "-d $internalCIDR" | grep -E -- "--to-source $eip(\$| )" > /dev/null; then
            exec_cmd "$iptables_cmd -t nat -D HAIRPIN_SNAT -s $internalCIDR -d $internalCIDR -j SNAT --to-source $eip"
            echo "Hairpin SNAT rule deleted: $internalCIDR -> $eip"
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


# example usage:
# delete_tc_u32_filter "$EXTERNAL_INTERFACE" "1:0" "192.168.1.1" "src"
function delete_tc_u32_filter() {
    dev=$1
    qdisc_id=$2
    cidr=$3
    matchDirection=$4

    # tc -p -s -d filter show dev net2 parent $qdisc_id
    # filter protocol ip pref 10 u32 chain 0
    # filter protocol ip pref 10 u32 chain 0 fh 800: ht divisor 1
    # filter protocol ip pref 10 u32 chain 0 fh 800::800 order 2048 key ht 800 bkt 0 *flowid :1 not_in_hw
    #   match IP dst 172.18.11.2/32
    #  police 0x1 rate 10Mbit burst 10Mb mtu 2Kb action drop overhead 0b linklayer ethernet
    #         ref 1 bind 1  installed 47118 sec used 47118 sec firstused 18113444 sec

    #  Sent 0 bytes 0 pkts (dropped 0, overlimits 0)

    # get the corresponding filterID by the EIP, and use the filterID to delete the corresponding filtering rule.
    ipList=$(tc -p -s -d filter show dev $dev parent $qdisc_id | grep -E "match IP src|dst ([0-9]{1,3}\.){3}[0-9]{1,3}/[0-9]{1,2}")
    i=0
    echo "$ipList" | while read line; do
        i=$((i+1))
        if echo "$line" | grep "$matchDirection $cidr"; then
            result=$(tc -p -s -d filter show dev $dev parent $qdisc_id | grep "filter protocol ip pref [0-9]\+ u32 \(fh\|chain [0-9]\+ fh\) \(\w\+::\w\+\) *" | awk '{print $5,$10}' | sed -n $i"p")
            arr=($result)
            pref=${arr[0]}
            filterID=${arr[1]}
            exec_cmd "tc filter del dev $dev parent $qdisc_id protocol ip prio $pref handle $filterID u32"
            break
        fi
    done
}

function eip_ingress_qos_add() {
    # ingress:
    # external --> $EXTERNAL_INTERFACE --> qos -->
    # dst ip is iptables eip on $EXTERNAL_INTERFACE
    for rule in $@
    do
        arr=(${rule//,/ })
        v4ip=(${arr[0]//\// })
        priority=${arr[1]}
        rate=${arr[2]}
        burst=${arr[3]}
        dev="$EXTERNAL_INTERFACE"
        matchDirection="dst"
        tc qdisc add dev $dev ingress 2>/dev/nul || true
        # get qdisc id
        qdisc_id=$(tc qdisc show dev $dev ingress | awk '{print $3}')
        # del old filter
        tc -p -s -d filter show dev $dev parent $qdisc_id | grep -w $v4ip
        if [ "$?" -eq 0 ];then
            delete_tc_u32_filter $dev $qdisc_id $v4ip $matchDirection
        fi
        exec_cmd "tc filter add dev $dev parent $qdisc_id protocol ip prio $priority u32 match ip $matchDirection $v4ip police rate "$rate"Mbit burst "$burst"Mb drop flowid :1"
    done
}

function eip_egress_qos_add() {
    # egress:
    # $EXTERNAL_INTERFACE --> qos --> external
    # src ip is iptables eip on $EXTERNAL_INTERFACE
    for rule in $@
    do
        arr=(${rule//,/ })
        v4ip=(${arr[0]//\// })
        priority=${arr[1]}
        rate=${arr[2]}
        burst=${arr[3]}
        qdisc_id="1:0"
        matchDirection="src"
        dev="$EXTERNAL_INTERFACE"
        tc qdisc add dev $dev root handle $qdisc_id htb 2>/dev/nul || true
        # del old filter
        tc -p -s -d filter show dev $dev parent $qdisc_id | grep -w $v4ip
        if [ "$?" -eq 0 ];then
            delete_tc_u32_filter $dev $qdisc_id $v4ip $matchDirection
        fi
        exec_cmd "tc filter add dev $dev parent $qdisc_id protocol ip prio $priority u32 match ip $matchDirection $v4ip police rate "$rate"Mbit burst "$burst"Mb drop flowid :1"
    done
}

function qos_add() {
    for rule in $@
    do
        IFS=',' read -r -a arr <<< "$rule"
        local qdiscType=(${arr[0]})
        local dev=${arr[1]}
        local priority=${arr[2]}
        local classifierType=${arr[3]}
        local matchType=${arr[4]}
        local matchDirection=${arr[5]}
        local cidr=${arr[6]}
        local rate=${arr[7]}
        local burst=${arr[8]}

        if [ "$qdiscType" == "ingress" ];then
            tc qdisc add dev $dev ingress 2>/dev/null || true
            # get qdisc id
            qdisc_id=$(tc qdisc show dev $dev ingress | awk '{print $3}')
        elif [ "$qdiscType" == "egress" ];then
            qdisc_id="1:0"
            tc qdisc add dev $dev root handle $qdisc_id htb 2>/dev/null || true
        fi

        if [ "$classifierType" == "u32" ];then
            tc -p -s -d filter show dev $dev parent $qdisc_id | grep -w $cidr
            if [ "$?" -ne 0 ];then
                exec_cmd "tc filter add dev $dev parent $qdisc_id protocol ip prio $priority u32 match $matchType $matchDirection $cidr police rate "$rate"Mbit burst "$burst"Mb drop flowid :1"
            fi
        elif [ "$classifierType" == "matchall" ];then
            tc -p -s -d filter show dev $dev parent $qdisc_id | grep -w matchall
            if [ "$?" -ne 0 ];then
                exec_cmd "tc filter add dev $dev parent $qdisc_id protocol ip prio $priority matchall action police rate "$rate"Mbit burst "$burst"Mb drop flowid :1"
            fi
        fi
    done
}

function qos_del() {
    for rule in $@
    do
        IFS=',' read -r -a arr <<< "$rule"
        local qdiscType=(${arr[0]})
        local dev=${arr[1]}
        local priority=${arr[2]}
        local classifierType=${arr[3]}
        local matchType=${arr[4]}
        local matchDirection=${arr[5]}
        local cidr=${arr[6]}
        local rate=${arr[7]}
        local burst=${arr[8]}

        if [ "$qdiscType" == "ingress" ];then
            qdisc_id=$(tc qdisc show dev $dev ingress | awk '{print $3}')
            if [ -z "$qdisc_id" ]; then
                exit 0
            fi
        elif [ "$qdiscType" == "egress" ];then
            qdisc_id="1:0"
        fi
        # if qdisc_id is empty, this means ingress qdisc is not added, so we don't need to delete filter.
        if [ "$classifierType" == "u32" ];then
            delete_tc_u32_filter $dev $qdisc_id $cidr $matchDirection
        elif [ "$classifierType" == "matchall" ];then
            tc -p -s -d filter show dev $dev parent $qdisc_id | grep -w matchall
            if [ "$?" -eq 0 ];then
                exec_cmd "tc filter del dev $dev parent $qdisc_id protocol ip prio $priority matchall"
            fi
        fi
    done
}

function eip_ingress_qos_del() {
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=(${arr[0]//\// })
        matchDirection="dst"
        dev="$EXTERNAL_INTERFACE"
        qdisc_id=$(tc qdisc show dev $dev ingress | awk '{print $3}')
        # if qdisc_id is empty, this means ingress qdisc is not added, so we don't need to delete filter.
        if [ -n "$qdisc_id" ]; then
            delete_tc_u32_filter $dev $qdisc_id $cidr $matchDirection
        fi
    done
}

function eip_egress_qos_del() {
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=(${arr[0]//\// })
        matchDirection="src"
        qdisc_id="1:0"
        dev="$EXTERNAL_INTERFACE"
        delete_tc_u32_filter $dev $qdisc_id $cidr $matchDirection
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
    hairpin-snat-add)
        echo "hairpin-snat-add $rules"
        add_hairpin_snat $rules
        ;;
    hairpin-snat-del)
        echo "hairpin-snat-del $rules"
        del_hairpin_snat $rules
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
