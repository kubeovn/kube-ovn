#!/usr/bin/env bash
set -euo pipefail
ENABLE_SSL=${ENABLE_SSL:-false}

./kube-ovn-monitor $@
