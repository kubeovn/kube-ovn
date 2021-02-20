#!/usr/bin/env bash

function exec_cmd() {
    cmd=${@:1:${#}}
    $cmd
    ret=$?
    if [ $ret -ne 0 ]; then
        echo "failed to exec \"$cmd\""
        exit $1
    fi
}

function init() {
    ip link set net1 up

    # add static chain
    iptables -t nat -N DNAT_FILTER
    iptables -t nat -N SNAT_FILTER
    iptables -t nat -N EXCLUSIVE_DNAT # floatingIp DNAT
    iptables -t nat -N EXCLUSIVE_SNAT # floatingIp SNAT
    iptables -t nat -N SHARED_DNAT
    iptables -t nat -N SHARED_SNAT

    iptables -t nat -A PREROUTING -j DNAT_FILTER
    iptables -t nat -A DNAT_FILTER -j EXCLUSIVE_DNAT
    iptables -t nat -A DNAT_FILTER -j SHARED_DNAT

    iptables -t nat -A POSTROUTING -j SNAT_FILTER
    iptables -t nat -A SNAT_FILTER -j EXCLUSIVE_SNAT
    iptables -t nat -A SNAT_FILTER -j SHARED_SNAT
}

function add_vpc_internal_route() {
    eipNum=`ip addr show net1 | grep inet | grep -v inet6 | awk '{print $2}' | cut -f1 -d'/' | wc -l`
    if [ $eipNum -eq 0 ];then
        return
    fi

    tableMax=`expr 99 + $eipNum`
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}
        nextHop=${arr[1]}

        for i in $(seq 100 $tableMax)
        do
            exec_cmd "ip ro replace $cidr via $nextHop dev eth0 table $i"
        done
    done
}

function del_vpc_internal_route() {
    eipNum=`ip addr show net1 | grep inet | grep -v inet6 | awk '{print $2}' | cut -f1 -d'/' | wc -l`
    if [ $eipNum -eq 0 ];then
        return
    fi

    tableMax=`expr 99 + $eipNum`
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}

        for i in $(seq 100 $tableMax)
        do
            exec_cmd "ip ro del $cidr table $i"
        done
    done
}

function add_eip() {
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=${arr[0]}
        gateway=${arr[1]}
        ruleTable=${arr[2]}

        exec_cmd "ip addr replace $eip dev net1"
        lines=`ip rule list table $ruleTable`
        if [ -n "$lines" ]; then
            exec_cmd "ip rule flush table $ruleTable"
        fi

        exec_cmd "ip ro replace default via $gateway dev net1 table $ruleTable"
        exec_cmd "ip rule add from $eip table $ruleTable"
        exec_cmd "ip rule add iif eth0 table $ruleTable"
    done
}

function del_eip() {
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=${arr[0]}
        lines=`ip addr show net1 | grep $eip`
        if [ -n "$lines" ]; then
            exec_cmd "ip addr del $eip dev net1"
        fi

        rTable=`ip rule | grep $eip|awk '{print $5}'`
        if [ -n "$rTable" ]; then
            exec_cmd "ip rule flush table $rTable"
        fi
    done
}

function sync_floating_ips() {
    iptables -t nat -F EXCLUSIVE_DNAT
    iptables -t nat -F EXCLUSIVE_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalIp=${arr[1]}

        exec_cmd "iptables -t nat -A EXCLUSIVE_DNAT -d $eip -j DNAT --to-destination $internalIp"
        exec_cmd "iptables -t nat -A EXCLUSIVE_SNAT -s $internalIp -j SNAT --to-source $eip"
    done
}

function sync_snat() {
    iptables -t nat -F SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalCIDR=${arr[1]}

        exec_cmd "iptables -t nat -A SHARED_SNAT -s $internalCIDR -j SNAT --to-source $eip"
    done
}

function sync_dnat() {
    iptables -t nat -F SHARED_DNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        dport=${arr[1]}
        protocol=${arr[2]}
        internalIp=${arr[3]}
        internalPort=${arr[4]}

        exec_cmd "iptables -t nat -A SHARED_DNAT -p $protocol -d $eip --dport $dport -j DNAT --to-destination $internalIp:$internalPort"
    done
}

rules=${@:2:${#}}
opt=$1
case $opt in
 init)
        echo "init"
        init
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
 dnat-sync)
        echo "dnat-sync $rules"
        sync_dnat $rules
        ;;
 snat-sync)
        echo "snat-sync $rules"
        sync_snat $rules
        ;;
 flouting-ip-sync)
        echo "flouting-ip-sync $rules"
        sync_floating_ips $rules
        ;;
 *)
        echo "Usage: $0 [init|subnet-route-add|subnet-route-del|eip-add|eip-del|dnat-sync|snat-sync|flouting-ip-sync] ..."
        exit 1
        ;;
esac
