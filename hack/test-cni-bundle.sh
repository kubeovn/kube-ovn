#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp -d)
trap 'rm -rf -- "$fixture"' EXIT
mkdir -p "$fixture/root/installer-cni"
if [[ -n "${CNI_BUNDLE_TEST_BINARY:-}" ]]; then
  cp "$CNI_BUNDLE_TEST_BINARY" "$fixture/kube-ovn"
else
  (cd "$repo_root" && CGO_ENABLED=0 GOMAXPROCS=2 go build -p 1 -o "$fixture/kube-ovn" ./cmd/cni)
fi
bash "$repo_root/dist/images/install-cni-bundle.sh" \
  "$fixture/kube-ovn" "$fixture/root/installer-cni/kube-ovn"

# Simulate a differently mounted CNI directory. The isolated host has no shell,
# executable helpers, ELF interpreter, or shared libraries.
mv "$fixture/root/installer-cni" "$fixture/root/host-cni"
cni_target="$fixture/root/host-cni/kube-ovn"
old_executable=$(readlink -f "$cni_target")
chroot_binary=$(command -v chroot)
CNI_COMMAND=VERSION PATH=/missing-host-tools "$chroot_binary" "$fixture/root" /host-cni/kube-ovn | python3 -c 'import json,sys; assert "1.0.0" in json.load(sys.stdin)["supportedVersions"]'

# An upgrade keeps the prior binary usable and publishes a distinct new one.
bash "$repo_root/dist/images/install-cni-bundle.sh" "$fixture/kube-ovn" "$cni_target"
[[ "$(readlink -f "$cni_target")" != "$old_executable" ]]
CNI_COMMAND=VERSION PATH=/missing-host-tools "$old_executable" > /dev/null

# A failed copy must leave the active executable unchanged.
active_executable=$(readlink -f "$cni_target")
if bash "$repo_root/dist/images/install-cni-bundle.sh" \
  "$fixture/missing-binary" "$cni_target" > /dev/null 2>&1; then
  echo 'Installer unexpectedly published an invalid CNI bundle' >&2
  exit 1
fi
[[ "$(readlink -f "$cni_target")" == "$active_executable" ]]
echo 'CNI shellless install, relocation, upgrade and failed-publication checks passed.'
