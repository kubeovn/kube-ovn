#!/usr/bin/env bash

iptables_cmd=$(which iptables)
iptables_save_cmd=$(which iptables-save)
if iptables-legacy -t nat -S INPUT 1 2>/dev/null; then
    # use iptables-legacy for centos 7
    iptables_cmd=$(which iptables-legacy)
    iptables_save_cmd=$(which iptables-legacy-save)
fi

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

    # Send gratuitous ARP for all the IPs on the external network interface at initialization
    # This is especially useful to update the MAC of the nexthop we announce to the BGP speaker 
    ip -4 addr show dev $EXTERNAL_INTERFACE | awk '/inet /{print $2}' | cut -d/ -f1 | xargs -n1 arping -I $EXTERNAL_INTERFACE -c 3 -U
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

        exec_cmd "ip route replace $cidr via $nextHop dev eth0"
    done
}

function del_vpc_internal_route() {
    # make sure inited
    check_inited
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}

        exec_cmd "ip route del $cidr dev eth0"
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
    #     exec_cmd "ip route del $cidr dev net1"
    #     sleep 1
    #     exec_cmd "ip route del default dev net1"
    # done
}

function add_eip() {
    # make sure inited
   check_inited
    for rule in $@
    do
        eip=${rule}
        eip_without_prefix=(${eip//\// })
        exec_cmd "ip addr replace $eip dev net1"
        exec_cmd "arping -I net1 -c 3 -U $eip_without_prefix"
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
        ipCidr=`ip addr show net1 | grep $eip | awk '{print $2 }'`
        if [ -n "$ipCidr" ]; then
            exec_cmd "ip addr del $ipCidr dev net1"
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
        # check if already exist
        $iptables_save_cmd | grep SHARED_SNAT | grep "\-s $internalCIDR" | grep "source $eip" && exit 0
        exec_cmd "$iptables_cmd -t nat -A SHARED_SNAT -o net1 -s $internalCIDR -j SNAT --to-source $eip $randomFullyOption"
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


# example usage:
# delete_tc_u32_filter "net1" "1:0" "192.168.1.1" "src"
function delete_tc_u32_filter() {
    dev=$1
    qdisc_id=$2
    cidr=$3
    matchDirection=$4

    # tc -p -s -d filter show dev net1 parent $qdisc_id
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
    # external --> net1 --> qos -->
    # dst ip is iptables eip on net1
    for rule in $@
    do
        arr=(${rule//,/ })
        v4ip=(${arr[0]//\// })
        priority=${arr[1]}
        rate=${arr[2]}
        burst=${arr[3]}
        dev="net1"
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
    # net1 --> qos --> external
    # src ip is iptables eip on net1
    for rule in $@
    do
        arr=(${rule//,/ })
        v4ip=(${arr[0]//\// })
        priority=${arr[1]}
        rate=${arr[2]}
        burst=${arr[3]}
        qdisc_id="1:0"
        matchDirection="src"
        dev="net1"
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
        dev="net1"
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
        dev="net1"
        delete_tc_u32_filter $dev $qdisc_id $cidr $matchDirection
    done
}


rules=${@:2:${#}}
opt=$1
case $opt in
    init)
        echo "init $rules"
        init $rules
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
        echo "Usage: $0 [init|subnet-route-add|subnet-route-del|eip-add|eip-del|floating-ip-add|floating-ip-del|dnat-add|dnat-del|snat-add|snat-del] ..."
        exit 1
        ;;
esac
