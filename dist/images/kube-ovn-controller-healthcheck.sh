#!/bin/bash
set -euo pipefail

OVN_NB_DAEMON=/var/run/ovn/ovn-nbctl.$(cat /var/run/ovn/ovn-nbctl.pid).ctl ovn-nbctl --timeout=10 show > /dev/null

nc -z -w3 127.0.0.1 10660

kubectl version
