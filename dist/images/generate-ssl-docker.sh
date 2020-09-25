#!/usr/bin/env bash
set -euo pipefail

docker run --rm -v $PWD:/etc/ovn kubeovn/kube-ovn:v1.5.0 bash generate-ssl.sh
kubectl create secret generic -n kube-system kube-ovn-tls --from-file=cacert=cacert.pem --from-file=cert=ovn-cert.pem --from-file=key=ovn-privkey.pem
rm -rf cacert.pem ovn-cert.pem ovn-privkey.pem ovn-req.pem
