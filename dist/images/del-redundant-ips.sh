#!/bin/bash
# This is a script for deleting redundant crd IPS
# This script will exit with code 1 if check failed
# This script is recommended for regular check, i.e., crontab, in a temporary processing

set -euo pipefail

delIPSWithIP(){
  IPS=()
  IPNAME=$(kubectl get ips -o=jsonpath='{range .items[*]}{.spec.ipAddress}{","}{.metadata.name}{"\n"}{end}')
  for ipname in $IPNAME
  do
    ARRIN=(${ipname//,/ })
    if [ ${ARRIN[0]} == $1 ]; then
      echo "delete ips " ${ARRIN[1]}
      kubectl delete ips ${ARRIN[1]}
    fi
  done
}

IPS=()
IPSUBNET=$(kubectl get ips -o=jsonpath='{range .items[*]}{.spec.ipAddress}{","}{.spec.subnet}{"\n"}{end}')
for ipsubnet in $IPSUBNET
do
  ARRIN=(${ipsubnet//,/ })
  if [ ${ARRIN[1]} != "join" ]; then
    IPS+=(${ARRIN[0]})
  fi
done

PODS=()
PODIPNODEIP=$(kubectl get pods -A  -o=jsonpath='{range .items[*]}{.status.podIP}{","}{.status.hostIP}{"\n"}{end}')
for podnode in $PODIPNODEIP
do
  ARRIN=(${podnode//,/ })
  if [ ${ARRIN[0]} != ${ARRIN[1]} ]; then
    PODS+=(${ARRIN[0]})
  fi
done

DIP=()
for ip in "${IPS[@]}"
do
  IN=true
  for pod in "${PODS[@]}"
  do
    if [ "$ip" == "$pod" ]; then
      IN=false
      continue
    fi
  done
  if $IN; then
     DIP+=("$ip")
  fi
done

if [ ${#DIP[@]} == 0 ]; then
  echo "no redundant IPS"
  exit 0
else
  echo "listing redundant IPS:"
  for ip in "${DIP[@]}"
  do
    echo $ip  "$(kubectl get ips | grep $ip | awk '{print $1}')"
  done
fi

read -p "Do you want to proceed? (yes/no) " yn

case $yn in
	yes )
	  for ip in "${DIP[@]}"
    do
      delIPSWithIP "$ip"
    done
	  ;;
	no )
		exit;;
	* ) echo invalid response, exist;
		exit 1;;
esac
