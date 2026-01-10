#!/bin/sh

set -e

ARCH=${ARCH:-amd64}
CNI_PLUGINS_VERSION=${CNI_PLUGINS_VERSION:-v1.9.0}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.35.0}
GOBGP_VERSION=${GOBGP_VERSION:-4.1.0}


DEPS_DIR=/godeps

mkdir -p "$DEPS_DIR"

curl -sSf -L --retry 5 https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_VERSION}/cni-plugins-linux-${ARCH}-${CNI_PLUGINS_VERSION}.tgz | \
    tar -xz -C "$DEPS_DIR" ./loopback ./portmap ./macvlan

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
        loopback|macvlan|portmap)
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

cat "$TARGETS_FILE"
