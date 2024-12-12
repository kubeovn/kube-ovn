#!/bin/bash

set -ex

bfdd-control session new set mintx "${BFD_MIN_TX:-1000}"
bfdd-control session new set minrx "${BFD_MIN_RX:-1000}"
bfdd-control session new set multi "${BFD_MULTI:-3}"

PEER_IPS=($(echo "${BFD_PEER_IPS:-::}" | tr ',' ' '))
for ip in ${PEER_IPS[*]}; do
  bfdd-control allow ${ip}
done

bfdd-control log type command no
