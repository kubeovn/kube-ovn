#!/bin/bash

VLAN_ID=${VLAN_ID:-100}

ip link show br1 >/dev/null 2>&1
if [ $? -ne 0 ]; then
	ip link add br1 type bridge
	ip link set br1 up
fi

ip link show vlan1 >/dev/null 2>&1
if [ $? -ne 0 ]; then
	ip link add vlan1 type veth peer name vlan2
	ip link add vlan2.$VLAN_ID link vlan2 type vlan id $VLAN_ID
fi

ip link set vlan1 up
ip link set vlan2 up
ip link set vlan2.$VLAN_ID up master br1
ip link set eth1 master br1
