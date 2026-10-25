#!/bin/sh

set -e

ARCH=${ARCH:-amd64}
CNI_PLUGINS_VERSION=${CNI_PLUGINS_VERSION:-v1.9.1}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.37.1}
GOBGP_VERSION=${GOBGP_VERSION:-4.10.0}
DUMB_INIT_VERSION=${DUMB_INIT_VERSION:-1.2.5}


DEPS_DIR=/godeps
LICENSE_DIR="$DEPS_DIR/licenses"

mkdir -p "$DEPS_DIR"
mkdir -p "$LICENSE_DIR/cni-plugins" "$LICENSE_DIR/kubernetes" "$LICENSE_DIR/gobgp" "$LICENSE_DIR/dumb-init"

curl -sSf -L --retry 5 "https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_VERSION}/cni-plugins-linux-${ARCH}-${CNI_PLUGINS_VERSION}.tgz" | \
    tar -xz -C "$DEPS_DIR" ./loopback ./portmap ./macvlan ./ipvlan

curl -sSf -L --retry 5 "https://dl.k8s.io/${KUBECTL_VERSION}/kubernetes-client-linux-${ARCH}.tar.gz" | \
    tar -xz -C "$DEPS_DIR" --strip-components=3 kubernetes/client/bin/kubectl

curl -sSf -L --retry 5 "https://github.com/osrg/gobgp/releases/download/v${GOBGP_VERSION}/gobgp_${GOBGP_VERSION}_linux_${ARCH}.tar.gz" | \
    tar -xz -C "$DEPS_DIR" gobgp

curl -sSf -L --retry 5 "https://raw.githubusercontent.com/containernetworking/plugins/${CNI_PLUGINS_VERSION}/LICENSE" \
    -o "$LICENSE_DIR/cni-plugins/LICENSE"
curl -sSf -L --retry 5 "https://raw.githubusercontent.com/kubernetes/kubernetes/${KUBECTL_VERSION}/LICENSE" \
    -o "$LICENSE_DIR/kubernetes/LICENSE"
curl -sSf -L --retry 5 "https://raw.githubusercontent.com/osrg/gobgp/v${GOBGP_VERSION}/LICENSE" \
    -o "$LICENSE_DIR/gobgp/LICENSE"
curl -sSf -L --retry 5 "https://raw.githubusercontent.com/Yelp/dumb-init/v${DUMB_INIT_VERSION}/LICENSE" \
    -o "$LICENSE_DIR/dumb-init/LICENSE"

cat > "$DEPS_DIR/THIRD_PARTY_NOTICES" <<EOF
Kube-OVN includes the following third-party binary components in this image.
The corresponding license texts are in the directories below.

CNI plugins ${CNI_PLUGINS_VERSION}
  License: Apache-2.0
  Source: https://github.com/containernetworking/plugins/tree/${CNI_PLUGINS_VERSION}
  License text: third-party/cni-plugins/LICENSE

kubectl ${KUBECTL_VERSION}
  License: Apache-2.0
  Source: https://github.com/kubernetes/kubernetes/tree/${KUBECTL_VERSION}
  License text: third-party/kubernetes/LICENSE

GoBGP ${GOBGP_VERSION}
  License: Apache-2.0
  Source: https://github.com/osrg/gobgp/tree/v${GOBGP_VERSION}
  License text: third-party/gobgp/LICENSE

dumb-init ${DUMB_INIT_VERSION}
  License: MIT
  Source: https://github.com/Yelp/dumb-init/tree/v${DUMB_INIT_VERSION}
  License text: third-party/dumb-init/LICENSE
EOF

ls -lh "$DEPS_DIR"

trivy rootfs --ignore-unfixed --scanners vuln --pkg-types library -f json --output trivy.json "$DEPS_DIR"

cat trivy.json

TARGETS_FILE="$DEPS_DIR/trivy-targets.txt"

: > "$TARGETS_FILE"
jq -r '.Results[] | select((.Type=="gobinary") and (.Vulnerabilities!=null)) | .Target' trivy.json | while read -r f; do
    name=$(basename "$f")
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
