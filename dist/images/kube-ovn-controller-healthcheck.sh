#!/bin/bash
set -euo pipefail

OVN_NB_DAEMON=/var/run/openvswitch/ovn-nbctl.$(cat /var/run/openvswitch/ovn-nbctl.pid).ctl ovn-nbctl --timeout=10 show > /dev/null

nc -z -w3 127.0.0.1 10660

nc -z -w3 "$KUBERNETES_SERVICE_HOST" "$KUBERNETES_SERVICE_PORT"
