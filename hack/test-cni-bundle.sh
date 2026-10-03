#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf -- "$fixture"' EXIT
mkdir -p "$fixture/sources" "$fixture/installer-cni"

# Use real dynamically linked ELF binaries to exercise the library/loader copy.
cp /bin/echo "$fixture/sources/ovs-vsctl"
cp /bin/echo "$fixture/sources/ethtool"
PATH="$fixture/sources:$PATH" bash "$repo_root/dist/images/install-cni-bundle.sh" \
  /bin/true "$fixture/installer-cni/kube-ovn"

# Simulate a host CNI directory mounted at a different path in the init container.
mv "$fixture/installer-cni" "$fixture/host-cni"
cni_target="$fixture/host-cni/kube-ovn"
old_executable=$(readlink -f "$cni_target")
for tool in ovs-vsctl ethtool; do
  env PATH=/missing-host-tools "${old_executable%/*}/tools/$tool" --version > /dev/null
done
"$cni_target"

# An upgrade keeps the prior bundle usable and publishes a distinct new one.
PATH="$fixture/sources:$PATH" bash "$repo_root/dist/images/install-cni-bundle.sh" \
  /bin/true "$cni_target"
[[ "$(readlink -f "$cni_target")" != "$old_executable" ]]
"$old_executable"
env PATH=/missing-host-tools "${old_executable%/*}/tools/ovs-vsctl" --version > /dev/null

# A dependency packaging failure must leave the active executable unchanged.
active_executable=$(readlink -f "$cni_target")
printf '#!/bin/sh\nexit 1\n' > "$fixture/sources/ovs-vsctl"
if PATH="$fixture/sources:$PATH" bash "$repo_root/dist/images/install-cni-bundle.sh" \
  /bin/true "$cni_target" > /dev/null 2>&1; then
  echo 'Installer unexpectedly published an invalid tool bundle' >&2
  exit 1
fi
[[ "$(readlink -f "$cni_target")" == "$active_executable" ]]
"$cni_target"
echo 'CNI bundle install, relocation, upgrade and failed-publication checks passed.'
