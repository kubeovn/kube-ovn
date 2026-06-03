#!/bin/sh

set -e

ARCH=${ARCH:-amd64}
CNI_PLUGINS_VERSION=${CNI_PLUGINS_VERSION:-v1.9.1}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.36.1}
GOBGP_VERSION=${GOBGP_VERSION:-4.5.0}


DEPS_DIR=/godeps

mkdir -p "$DEPS_DIR"

curl -sSf -L --retry 5 https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_VERSION}/cni-plugins-linux-${ARCH}-${CNI_PLUGINS_VERSION}.tgz | \
    tar -xz -C "$DEPS_DIR" ./loopback ./portmap ./macvlan ./ipvlan

curl -sSf -L --retry 5 https://dl.k8s.io/${KUBECTL_VERSION}/kubernetes-client-linux-${ARCH}.tar.gz | \
    tar -xz -C "$DEPS_DIR" --strip-components=3 kubernetes/client/bin/kubectl

curl -sSf -L --retry 5 https://github.com/osrg/gobgp/releases/download/v${GOBGP_VERSION}/gobgp_${GOBGP_VERSION}_linux_${ARCH}.tar.gz | \
    tar -xz -C "$DEPS_DIR" gobgp

ls -lh "$DEPS_DIR"

trivy rootfs --ignore-unfixed --scanners vuln --pkg-types library -f json --output trivy.json "$DEPS_DIR"

cat trivy.json

TARGETS_FILE="$DEPS_DIR/trivy-targets.txt"

: > "$TARGETS_FILE"
jq -r '.Results[] | select((.Type=="gobinary") and (.Vulnerabilities!=null)) | .Target' trivy.json | while read f; do
    name=$(basename $f)
    case $name in
        loopback|macvlan|portmap|ipvlan)
            echo "$name@$CNI_PLUGINS_VERSION" >> "$TARGETS_FILE"
            ;;
        kubectl)
            echo "$name@$KUBECTL_VERSION" >> "$TARGETS_FILE"
            ;;
        gobgp)
            echo "$name@v$GOBGP_VERSION" >> "$TARGETS_FILE"
            ;;
        *)
            echo "Unknown go binary: $f"
            exit 1
            ;;
    esac
done

# Always rebuild the CNI plugins from source. The trivy scan above only enlists
# binaries that are vulnerable at build time, but the upstream prebuilt plugins
# embed whatever Go toolchain they were released with, so a plugin that is clean
# today silently ships an outdated stdlib and becomes vulnerable once a new stdlib
# CVE is published. The plugins are cheap to build (unlike kubectl), so rebuild
# them unconditionally instead of leaving them pinned to the upstream binaries.
for name in loopback macvlan portmap ipvlan; do
    grep -q "^$name@" "$TARGETS_FILE" || echo "$name@$CNI_PLUGINS_VERSION" >> "$TARGETS_FILE"
done

cat "$TARGETS_FILE"
