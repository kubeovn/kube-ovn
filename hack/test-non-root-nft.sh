#!/usr/bin/env bash
set -euo pipefail

# Exercise the installed nft executable with the daemon UID and only NET_ADMIN.
# The disposable network namespace keeps the test independent of host rules.
docker run --rm --network none --user 65534:65534 \
  --cap-drop ALL --cap-add NET_ADMIN --entrypoint /bin/bash "${1:?image is required}" \
  -euo pipefail -c '
    test "$(id -u)" -eq 65534
    while read -r key value rest; do
      case "$key" in
        CapInh:|CapAmb:) test "$value" = 0000000000000000 ;;
      esac
    done < /proc/self/status
    nft add table inet kube_ovn_nonroot_test
    nft list table inet kube_ovn_nonroot_test
    nft delete table inet kube_ovn_nonroot_test
  '
