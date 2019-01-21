#!/usr/bin/env bash
set -euo pipefail

./kube-ovn-controller --ovn-nb-host=${OVN_NB_SERVICE_HOST} --ovn-nb-port=${OVN_NB_SERVICE_PORT}