#!/bin/bash

set +e

echo "1) check cni configuration"
if [ ! -e "/etc/cni/net.d" ]; then
  echo "Directory /etc/cni/net.d does not exist, please check kube-ovn-cni pod status"
fi
for file in $(ls "/etc/cni/net.d")
do
  if [[ ! $file =~ "kube-ovn.conflist" ]]; then
    echo "Check files in /etc/cni/net.d, make sure if the config file $file should be deleted"
  fi
done

echo "2) check system ipv4 config"
probe_mtu=`cat /proc/sys/net/ipv4/tcp_mtu_probing`
if [ $probe_mtu == 0 ]; then
  echo "The 'tcp_mtu_probing' config may affect traffic, make sure if /proc/sys/net/ipv4/tcp_mtu_probing should be set to 1"
fi
if [ -e /proc/sys/net/ipv4/tcp_tw_recycle ]; then
  recycle=`cat /proc/sys/net/ipv4/tcp_tw_recycle`
  if [ $recycle == 1 ]; then
    echo "The 'tcp_tw_recycle' config affects nodeport service, make sure change /proc/sys/net/ipv4/tcp_tw_recycle to 0"
  fi
fi

echo "3) check checksum value"
which netstat 2>/dev/null >/dev/null
if [[ $? != 0 ]]; then
  echo "The netstat cmd not found, maybe can be installed mannully and exec 'netstat -s' to check if there is 'InCsumErrors'"
  echo "If there's 'InCsumErrors' and the value is increasing, should exec cmd 'ethtool -K ETH tx off' to disable checksum, where 'ETH' is the nic used for traffics"
else
  result=`netstat -s`
  if [[ $result =~ "InCsumErrors" ]]; then
    echo "Found 'InCsumErrors' para after exec 'netstat -s' cmd, check if the value is increasing, maybe should exec cmd 'ethtool -K ETH tx off' to disable checksum, where 'ETH' is the nic used for traffics"
  fi
fi

echo "4) check dns config"
result=`cat /etc/resolv.conf`
if [[ $result =~ ".com" ]]; then
  echo "There's *.com in dns search name, make sure the config /etc/resolv.conf is right"
fi

echo "5) check firewall config"
result=`ps -ef | grep firewall | wc -l`
if [[ $result > 1 ]]; then
  echo "The firewalld is running, make sure it has no effect on traffics across nodes"
fi

result=`ps -ef | grep security | wc -l`
if [[ $result > 1 ]]; then
  echo "Found pid with '*security*' name, make sure it has no effect on traffics"
fi
result=`ps -ef | grep qax | wc -l`
if [[ $result > 1 ]]; then
  echo "Found pid with '*qax*' name, make sure it has no effect on traffics"
fi
result=`ps -ef | grep safe | wc -l`
if [[ $result > 1 ]]; then
  echo "Found pid with '*safe*' name, make sure it has no effect on traffics"
fi
result=`ps -ef | grep defence | wc -l`
if [[ $result > 1 ]]; then
  echo "Found pid with '*defence*' name, make sure it has no effect on traffics"
fi
result=`ps -ef | grep vmsec | wc -l`
if [[ $result > 1 ]]; then
  echo "Found pid with '*vmsec*' name, make sure it has no effect on traffics"
fi

echo "6) check geneve 6081 connection"
which nmap 2>/dev/null >/dev/null
if [[ $? != 0 ]]; then
  echo "The nmap cmd not found, maybe can be installed mannully and exec 'nmap -sU 127.0.0.1 -p 6081' to check port connection"
else
  result=`nmap -sU 127.0.0.1 -p 6081`
  if [[ ! $result =~ "open" ]]; then
    echo "The 6081 port for geneve encapsulation may be not available, if the number of nodes is more than 1, please check if ovs-ovn pod is healthy"
  fi
fi
