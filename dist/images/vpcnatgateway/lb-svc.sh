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
    ip link set net1 up
    ip link set dev net1 arp off

    if [ $(ip rule show iif net1 | wc -l) -eq 0 ]; then
        exec_cmd "ip rule add iif net1 table $ROUTE_TABLE"
    fi
    if [ $(ip rule show iif eth0 | wc -l) -eq 0 ]; then
        exec_cmd "ip rule add iif eth0 table $ROUTE_TABLE"
    fi
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
        ip link set dev net1 arp on
        exec_cmd "arping -c 3 -s $eip_without_prefix $gateway"
    done
}

function add_dnat() {
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        dport=${arr[1]}
        protocol=${arr[2]}
        internalIp=${arr[3]}
        internalPort=${arr[4]}
        defaultGateway=${arr[5]}
        # check if already exist
        iptables-save  | grep "PREROUTING" | grep "\-d $eip" | grep "p $protocol" | grep "dport $dport"| grep "destination $internalIp:$internalPort" && exit 0
        exec_cmd "iptables -t nat -A PREROUTING -p $protocol -d $eip --dport $dport -j DNAT --to-destination $internalIp:$internalPort"
 
        exec_cmd "ip route replace $internalIp via $defaultGateway table $ROUTE_TABLE"
 
        iptables-save  | grep "POSTROUTING" | grep "\-d $internalIp" | grep "MASQUERADE" && exit 0
        exec_cmd "iptables -t nat -I POSTROUTING -d $internalIp -j MASQUERADE"
    done
}

rules=${@:2:${#}}
opt=$1
case $opt in
 init)
        echo "init $rules"
        init $rules
        ;;
 eip-add)
        echo "eip-add $rules"
        add_eip $rules
        ;;
 dnat-add)
        echo "dnat-add $rules"
        add_dnat $rules
        ;;
 *)
        echo "Usage: $0 [init|eip-add|dnat-add] ..."
        exit 1
        ;;
esac
