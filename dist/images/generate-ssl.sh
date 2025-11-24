#!/usr/bin/env bash
set -euo pipefail

ovs-pki init -l /dev/stdout --force
cp /var/lib/openvswitch/pki/switchca/private/cakey.pem /etc/ovn/
cp /var/lib/openvswitch/pki/switchca/cacert.pem /etc/ovn/
cd /etc/ovn
ovs-pki req ovn -l /dev/stdout --force
ovs-pki -b sign ovn -l /dev/stdout --force
chmod 666 cakey.pem cacert.pem ovn-req.pem ovn-cert.pem ovn-privkey.pem
