#!/usr/bin/env bash
set -euo pipefail

# Generate the dedicated cert-manager fixture CA, separate from OVN TLS PKI.
# ovs-pki's text-prefixed output and implicit signing usage do not satisfy the
# IPsec agent's strict PEM and CA profile checks.
ipsec_ca_dir=${1:?an output directory is required}
umask 077
for name in ipsec-cakey.pem ipsec-cacert.pem; do
  if [[ -e "$ipsec_ca_dir/$name" || -L "$ipsec_ca_dir/$name" ]]; then
    echo "Refusing to replace an existing IPsec CA file: $name" >&2
    exit 1
  fi
done
openssl req -x509 -newkey rsa:2048 -nodes -sha256 -days 3650 \
  -subj '/O=kubeovn/CN=Kube-OVN IPsec CA' \
  -addext 'basicConstraints=critical,CA:TRUE' \
  -addext 'keyUsage=critical,keyCertSign,cRLSign' \
  -keyout "$ipsec_ca_dir/ipsec-cakey.pem" \
  -out "$ipsec_ca_dir/ipsec-cacert.pem" 2>/dev/null
