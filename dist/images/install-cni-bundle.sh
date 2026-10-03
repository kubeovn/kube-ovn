#!/usr/bin/env bash
set -euo pipefail

# The statically linked CNI uses OVSDB and kernel APIs directly. Publish an
# immutable binary so in-flight invocations retain their version during upgrade.
cni_source=$1
cni_target=$2
bundle_root="${cni_target%/*}/.kube-ovn-bundles"
mkdir -p "$bundle_root"
bundle=$(mktemp -d "$bundle_root/bundle.XXXXXXXX")
cp "$cni_source" "$bundle/kube-ovn"
CNI_COMMAND=VERSION "$bundle/kube-ovn" > /dev/null

# The host CNI directory may be mounted at a different path in the installer.
ln -s ".kube-ovn-bundles/${bundle##*/}/kube-ovn" "${cni_target}.bundle.$$"
mv -Tf "${cni_target}.bundle.$$" "$cni_target"
