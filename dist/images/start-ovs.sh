#!/bin/bash

rpm -i /root/kmod.rpm

/usr/share/openvswitch/scripts/ovs-ctl.sh start
modprobe vport-geneve

tail -f /dev/null