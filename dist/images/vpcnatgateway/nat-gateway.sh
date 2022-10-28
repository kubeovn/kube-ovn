#!/usr/bin/env bash

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
    # run once is enough
    iptables-save | grep DNAT_FILTER && exit 0
    # add static chain
    # this also a flag to make sure init once
    iptables -t nat -N DNAT_FILTER
    ip link set net1 up
    ip link set dev net1 arp off

    # add static chain
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


function get_iptables_version() {
  exec_cmd "iptables --version"
}

function add_vpc_internal_route() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
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
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}

        exec_cmd "ip route del $cidr dev eth0"
    done
}

function add_vpc_external_route() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        cidr=${arr[0]}
        nextHop=${arr[1]}

        exec_cmd "ip route replace default via $nextHop dev net1"
        # replace default route to dev net1 to access public network
        # TODO: use multus macvlan to provider eth0 to skip this external route
        sleep 1
        ip route | grep "default via $nextHop dev net1"
        # make sure route is added
        # gw lost probably occured when you create >10 nat gw pod at the same time
        # so add the same logic again in every eip add process

    done
}

function del_vpc_external_route() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
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
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=${arr[0]}
        eip_without_prefix=(${eip//\// })
        eip_network=$(ipcalc -n $eip | awk -F '=' '{print $2}')
        eip_prefix=$(ipcalc -p $eip | awk -F '=' '{print $2}')
        gateway=${arr[1]}

        exec_cmd "ip addr replace $eip dev net1"
        ip link set dev net1 arp on
        # gw may lost, even if add_vpc_external_route add route successfully
        exec_cmd "ip route replace default via $gateway dev net1"
        ip route | grep "default via $gateway dev net1"
        exec_cmd "arping -I net1 -c 3 -D $eip_without_prefix"
    done
}

function del_eip() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
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
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalIp=${arr[1]}
        # check if already exist
        iptables-save  | grep "EXCLUSIVE_DNAT" | grep -w "\-d $eip/32" | grep  "destination" && exit 0
        exec_cmd "iptables -t nat -A EXCLUSIVE_DNAT -d $eip -j DNAT --to-destination $internalIp"
        exec_cmd "iptables -t nat -A EXCLUSIVE_SNAT -s $internalIp -j SNAT --to-source $eip"
    done
}

function del_floating_ip() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalIp=${arr[1]}
        # check if already exist
        iptables-save  | grep "EXCLUSIVE_DNAT" | grep -w "\-d $eip/32" | grep  "destination"
        if [ "$?" -eq 0 ];then
            exec_cmd "iptables -t nat -D EXCLUSIVE_DNAT -d $eip -j DNAT --to-destination $internalIp"
            exec_cmd "iptables -t nat -D EXCLUSIVE_SNAT -s $internalIp -j SNAT --to-source $eip"
            exec_cmd "conntrack -D -d $eip"
        fi
    done
}

function add_snat() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    # iptables -t nat -F SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalCIDR=${arr[1]}
        randomFullyOption=${arr[2]}
        # check if already exist
        iptables-save  | grep "SHARED_SNAT" | grep "\-s $internalCIDR" | grep "source $eip" && exit 0
        exec_cmd "iptables -t nat -A SHARED_SNAT -s $internalCIDR -j SNAT --to-source $eip $randomFullyOption"
    done
}
function del_snat() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    # iptables -t nat -F SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        internalCIDR=${arr[1]}
        # check if already exist
        ruleMatch=$(iptables-save  | grep "SHARED_SNAT" | grep "\-s $internalCIDR" | grep "source $eip")
        if [ "$?" -eq 0 ];then
          ruleMatch=$(echo $ruleMatch | sed 's/-A //')
          exec_cmd "iptables -t nat -D $ruleMatch"
        fi
    done
}


function add_dnat() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        dport=${arr[1]}
        protocol=${arr[2]}
        internalIp=${arr[3]}
        internalPort=${arr[4]}
        # check if already exist
        iptables-save  | grep "SHARED_DNAT" | grep -w "\-d $eip/32" | grep "p $protocol" | grep -w "dport $dport"| grep  -w "destination $internalIp:$internalPort"  && exit 0
        exec_cmd "iptables -t nat -A SHARED_DNAT -p $protocol -d $eip --dport $dport -j DNAT --to-destination $internalIp:$internalPort"
    done
}


function del_dnat() {
    # make sure inited
    iptables-save -t nat | grep  SNAT_FILTER | grep SHARED_SNAT
    for rule in $@
    do
        arr=(${rule//,/ })
        eip=(${arr[0]//\// })
        dport=${arr[1]}
        protocol=${arr[2]}
        internalIp=${arr[3]}
        internalPort=${arr[4]}
        # check if already exist
        iptables-save  | grep "SHARED_DNAT" | grep -w "\-d $eip/32" | grep "p $protocol" | grep -w "dport $dport"| grep  -w "destination $internalIp:$internalPort"
        if [ "$?" -eq 0 ];then
          exec_cmd "iptables -t nat -D SHARED_DNAT -p $protocol -d $eip --dport $dport -j DNAT --to-destination $internalIp:$internalPort"
        fi
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
 ext-subnet-route-add)
        echo "ext-subnet-route-add $rules"
        add_vpc_external_route $rules
        ;;
 ext-subnet-route-del)
        echo "ext-subnet-route-del $rules"
        del_vpc_external_route $rules
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
 *)
        echo "Usage: $0 [init|subnet-route-add|subnet-route-del|eip-add|eip-del|floating-ip-add|floating-ip-del|dnat-add|dnat-del|snat-add|snat-del] ..."
        exit 1
        ;;
esac
