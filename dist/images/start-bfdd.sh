#!/bin/bash

set -ex

POD_IPS=${POD_IPS:-::}
LISTEN_IPS=($(echo "${POD_IPS}" | tr ',' ' '))
LISTEN_ARGS=""
for ip in ${LISTEN_IPS[*]}; do
  LISTEN_ARGS="${LISTEN_ARGS} --listen=${ip}"
done

bfdd-beacon --nofork --tee ${LISTEN_ARGS}
