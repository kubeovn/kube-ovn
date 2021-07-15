#!/usr/bin/env bash
set -euo pipefail
exist=$(kubectl get secret -n kube-system kube-ovn-tls --ignore-not-found)
if [[ $exist == "" ]];then
  docker run --rm -v $PWD:/etc/ovn kubeovn/kube-ovn:v1.7.1 bash generate-ssl.sh
  kubectl create secret generic -n kube-system kube-ovn-tls --from-file=cacert=cacert.pem --from-file=cert=ovn-cert.pem --from-file=key=ovn-privkey.pem
  rm -rf cacert.pem ovn-cert.pem ovn-privkey.pem ovn-req.pem
fi
