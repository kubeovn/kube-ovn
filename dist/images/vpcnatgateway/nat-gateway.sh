#!/usr/bin/env bash

ROUTE_TABLE=100

function exec_cmd() {
    cmd=${@:1:${#}}
    $cmd
    ret=$?
    if [ $ret -ne 0 ]; then
        echo "failed to exec \"$cmd\""
        exit $ret
    fi
}

function init() {
    lanCIDR=$1
    ip link set net1 up
    if [ $(ip rule show iif net1 | wc -l) -eq 0 ]; then
        exec_cmd "ip rule add iif net1 table $ROUTE_TABLE"
    fi
    if [ $(ip rule show iif eth0 | wc -l) -eq 0 ]; then
        exec_cmd "ip rule add iif eth0 table $ROUTE_TABLE"
    fi
    exec_cmd "ip route replace $lanCIDR dev eth0 table $ROUTE_TABLE"

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
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}
        nextHop=${arr[1]}

        exec_cmd "ip route replace $cidr via $nextHop dev eth0 table $ROUTE_TABLE"
    done
}

function del_vpc_internal_route() {
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}

        exec_cmd "ip route del $cidr table $ROUTE_TABLE"
    done
}

function add_eip() {
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=${arr[0]}
        eip_without_prefix=(${eip//\// })
        eip_network=$(ipcalc -n $eip | awk -F '=' '{print $2}')
        eip_prefix=$(ipcalc -p $eip | awk -F '=' '{print $2}')
        gateway=${arr[1]}

        exec_cmd "ip addr replace $eip dev net1"
        exec_cmd "ip route replace $eip_network/$eip_prefix dev net1 table $ROUTE_TABLE"
        exec_cmd "ip route replace default via $gateway dev net1 table $ROUTE_TABLE"
        exec_cmd "arping -c 3 -s $eip_without_prefix $gateway"
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
 dnat-sync)
        echo "dnat-sync $rules"
        sync_dnat $rules
        ;;
 snat-sync)
        echo "snat-sync $rules"
        sync_snat $rules
        ;;
 floating-ip-sync)
        echo "floating-ip-sync $rules"
        sync_floating_ips $rules
        ;;
 *)
        echo "Usage: $0 [init|subnet-route-add|subnet-route-del|eip-add|eip-del|dnat-sync|snat-sync|floating-ip-sync] ..."
        exit 1
        ;;
esac