#!/bin/bash

function remove_conf(){
    rm -rf /etc/cni/net.d/01-kube-ovn.conflist
    rm -rf /kube-ovn/01-kube-ovn.conflist
    echo "cni health check is failed"
}

CNI_SOCK=/run/openvswitch/kube-ovn-daemon.sock
if [[ -e ${CNI_SOCK} ]]
then 
   echo "${CNI_SOCK} is exist"
else
   remove_conf
   exit 1
fi

STATUS_CODE=`curl -sIL -w "%{http_code}" -o /dev/null  http://127.0.0.1:10665/metrics`
if [ ${STATUS_CODE} -eq 200 ]
then 
    exit 0 
else
    remove_conf
    exit 1
fi
