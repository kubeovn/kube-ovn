#!/bin/sh

set -e

ARCH=${ARCH:-amd64}
CNI_PLUGINS_VERSION=${CNI_PLUGINS_VERSION:-v1.9.1}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.36.1}
GOBGP_VERSION=${GOBGP_VERSION:-4.5.0}


DEPS_DIR=/godeps

mkdir -p "$DEPS_DIR"

download_archive() {
    url=$1
    file=$(mktemp)
    curl -sSf -L --retry 5 --retry-all-errors --retry-delay 5 --connect-timeout 20 --max-time 300 -o "$file" "$url"
    echo "$file"
}

cni_archive=$(download_archive "https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_VERSION}/cni-plugins-linux-${ARCH}-${CNI_PLUGINS_VERSION}.tgz")
tar -xz -f "$cni_archive" -C "$DEPS_DIR" ./loopback ./portmap ./macvlan ./ipvlan
rm -f "$cni_archive"

kubectl_archive=$(download_archive "https://dl.k8s.io/${KUBECTL_VERSION}/kubernetes-client-linux-${ARCH}.tar.gz")
tar -xz -f "$kubectl_archive" -C "$DEPS_DIR" --strip-components=3 kubernetes/client/bin/kubectl
rm -f "$kubectl_archive"

gobgp_archive=$(download_archive "https://github.com/osrg/gobgp/releases/download/v${GOBGP_VERSION}/gobgp_${GOBGP_VERSION}_linux_${ARCH}.tar.gz")
tar -xz -f "$gobgp_archive" -C "$DEPS_DIR" gobgp
rm -f "$gobgp_archive"

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

cat "$TARGETS_FILE"
