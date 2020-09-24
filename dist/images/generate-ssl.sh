#!/usr/bin/env bash
set -euo pipefail

ovs-pki init --force
cp /var/lib/openvswitch/pki/switchca/cacert.pem /etc/ovn/
cd /etc/ovn
ovs-pki req ovn --force
ovs-pki -b sign ovn --force
