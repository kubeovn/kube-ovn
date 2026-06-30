#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

require_file() {
  local file="$1"
  [[ -f "$ROOT_DIR/$file" ]] || fail "missing $file"
}

require_pattern() {
  local file="$1"
  local pattern="$2"
  grep -Eq -- "$pattern" "$ROOT_DIR/$file" || fail "$file missing pattern: $pattern"
}

reject_pattern() {
  local file="$1"
  local pattern="$2"
  if grep -En -- "$pattern" "$ROOT_DIR/$file"; then
    fail "$file contains forbidden pattern: $pattern"
  fi
}

require_file dist/images/Dockerfile.base-almalinux10
require_pattern dist/images/Dockerfile.base-almalinux10 'FROM almalinux:10 AS ovs-builder'
require_pattern dist/images/Dockerfile.base-almalinux10 'FROM almalinux:10 AS runtime'
require_pattern dist/images/Dockerfile.base-almalinux10 'ARG ARCH'
require_pattern dist/images/Dockerfile.base-almalinux10 'ARG HTTPS_PROXY'
require_pattern dist/images/Dockerfile.base-almalinux10 'CNI_PLUGINS_VERSION='
require_pattern dist/images/Dockerfile.base-almalinux10 'KUBECTL_VERSION='
require_pattern dist/images/Dockerfile.base-almalinux10 'GOBGP_VERSION='
require_pattern dist/images/Dockerfile.base-almalinux10 'DUMB_INIT_VERSION='
require_pattern dist/images/Dockerfile.base-almalinux10 'rpm-build'
require_pattern dist/images/Dockerfile.base-almalinux10 'make rpm-fedora'
require_pattern dist/images/Dockerfile.base-almalinux10 '--without check --without dpdk --without afxdp --without usdt'
require_pattern dist/images/Dockerfile.base-almalinux10 "--define 'debug_package %\\{nil\\}'"
require_pattern dist/images/Dockerfile.base-almalinux10 'checkpolicy'
require_pattern dist/images/Dockerfile.base-almalinux10 'selinux-policy-devel'
require_pattern dist/images/Dockerfile.base-almalinux10 'COPY --from=ovs-builder /packages /packages'
require_pattern dist/images/Dockerfile.base-almalinux10 '/packages/openvswitch-\[0-9\]\*\.rpm'
require_pattern dist/images/Dockerfile.base-almalinux10 '/packages/python3-openvswitch-\*\.rpm'
require_pattern dist/images/Dockerfile.base-almalinux10 'dnf -y --setopt=install_weak_deps=False install[[:space:]\\]*'
require_pattern dist/images/Dockerfile.base-almalinux10 '/packages/ovn-\[0-9\]\*\.rpm'
require_pattern dist/images/Dockerfile.base-almalinux10 '/packages/ovn-central-\[0-9\]\*\.rpm'
require_pattern dist/images/Dockerfile.base-almalinux10 '/packages/ovn-host-\[0-9\]\*\.rpm'
require_pattern dist/images/Dockerfile.base-almalinux10 'iputils'
require_pattern dist/images/Dockerfile.base-almalinux10 'ipvsadm'
require_pattern dist/images/Dockerfile.base-almalinux10 'conntrack-tools'
require_pattern dist/images/Dockerfile.base-almalinux10 'ndisc6'
require_pattern dist/images/Dockerfile.base-almalinux10 'traceroute'
require_pattern dist/images/Dockerfile.base-almalinux10 'unbound-devel'
require_pattern dist/images/Dockerfile.base-almalinux10 'unbound-libs'
require_pattern dist/images/Dockerfile.base-almalinux10 'python3-netifaces'
require_pattern dist/images/Dockerfile.base-almalinux10 'python3-sortedcontainers'
require_pattern dist/images/Dockerfile.base-almalinux10 'strongswan'
require_pattern dist/images/Dockerfile.base-almalinux10 'fix-netdev-get-ifindex-type\.patch'
require_pattern dist/images/Dockerfile.base-almalinux10 'command -v conntrack'
require_pattern dist/images/Dockerfile.base-almalinux10 'command -v ndisc6'
require_pattern dist/images/Dockerfile.base-almalinux10 'command -v ovs-vswitchd'
require_pattern dist/images/iptables-wrapper-installer.sh 'for cmd in iptables iptables-save iptables-restore ip6tables ip6tables-save ip6tables-restore'
require_pattern dist/images/iptables-wrapper-installer.sh 'xtables-\\?\$\{mode\\?\}-multi'
require_pattern dist/images/iptables-wrapper-installer.sh '/usr/local/sbin/\\?\$\{cmd\\?\}'
reject_pattern dist/images/Dockerfile.base-almalinux10 'make install DESTDIR=/install-root'
reject_pattern dist/images/Dockerfile.base-almalinux10 'LEGACY|DEBUG|dpkg|apt-get|apt |Dockerfile\.base-dpdk'
reject_pattern dist/images/Dockerfile.base-almalinux10 'python3-pip|pip3 install|dnf -y remove gdb-gdbserver'
reject_pattern dist/images/Dockerfile.base-almalinux10 'ovn-central-\*\.rpm|ovn-host-\*\.rpm|/packages/ovn-\*\.rpm'
reject_pattern dist/images/Dockerfile.base-almalinux10 'openvswitch-selinux-policy|debuginfo|debugsource'
reject_pattern dist/images/Dockerfile.base-almalinux10 '/\^make selinux-policy\$/d|selinux\\/openvswitch-custom|%\{?package[[:space:]]+selinux-policy|%post.*selinux-policy|%files.*selinux-policy'

require_pattern makefiles/build.mk '^\.PHONY: base-amd64-almalinux10$'
require_pattern makefiles/build.mk '^base-amd64-almalinux10:$'
require_pattern makefiles/build.mk '^\.PHONY: base-arm64-almalinux10$'
require_pattern makefiles/build.mk '^base-arm64-almalinux10:$'
require_pattern makefiles/build.mk '^\.PHONY: base-tar-amd64-almalinux10$'
require_pattern makefiles/build.mk '^base-tar-amd64-almalinux10:$'
require_pattern makefiles/build.mk '^\.PHONY: base-tar-arm64-almalinux10$'
require_pattern makefiles/build.mk '^base-tar-arm64-almalinux10:$'
require_pattern makefiles/build.mk '^\.PHONY: build-kube-ovn-almalinux10$'
require_pattern makefiles/build.mk '^build-kube-ovn-almalinux10: gen-crd build-debug-almalinux10 build-go$'
require_pattern makefiles/build.mk '^\.PHONY: build-kube-ovn-arm64-almalinux10$'
require_pattern makefiles/build.mk '^build-kube-ovn-arm64-almalinux10: gen-crd build-debug-arm64-almalinux10 build-go-arm$'
require_pattern makefiles/build.mk '^\.PHONY: build-debug-almalinux10$'
require_pattern makefiles/build.mk '^\.PHONY: build-debug-arm64-almalinux10$'
require_pattern makefiles/build.mk '^\.PHONY: image-kube-ovn-almalinux10$'
require_pattern makefiles/build.mk '^image-kube-ovn-almalinux10: gen-crd image-kube-ovn-debug-almalinux10 build-go$'
require_pattern makefiles/build.mk '^\.PHONY: image-kube-ovn-arm64-almalinux10$'
require_pattern makefiles/build.mk '^image-kube-ovn-arm64-almalinux10: gen-crd image-kube-ovn-arm64-debug-almalinux10 build-go-arm$'
require_pattern makefiles/build.mk '^\.PHONY: image-kube-ovn-debug-almalinux10$'
require_pattern makefiles/build.mk '^\.PHONY: image-kube-ovn-arm64-debug-almalinux10$'
require_pattern makefiles/build.mk 'Dockerfile\.base-almalinux10'
require_pattern makefiles/build.mk 'PROXY_BUILD_ARGS'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(RELEASE_TAG\)-almalinux10-amd64'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(RELEASE_TAG\)-almalinux10-arm64'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(DEBUG_TAG\)-almalinux10-amd64'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(DEBUG_TAG\)-almalinux10-arm64'
require_pattern makefiles/build.mk 'kube-ovn:\$\(RELEASE_TAG\)-almalinux10'
require_pattern makefiles/build.mk 'kube-ovn:\$\(DEBUG_TAG\)-almalinux10'
require_pattern makefiles/build.mk '--build-arg BASE_TAG=\$\(RELEASE_TAG\)-almalinux10'
require_pattern makefiles/build.mk '--build-arg BASE_TAG=\$\(DEBUG_TAG\)-almalinux10'
require_pattern makefiles/build.mk '\$\(PROXY_BUILD_ARGS\).*almalinux10-amd64'
require_pattern makefiles/build.mk '\$\(PROXY_BUILD_ARGS\).*almalinux10-arm64'
require_pattern makefiles/build.mk 'image-amd64-almalinux10\.tar'
require_pattern makefiles/build.mk 'image-arm64-almalinux10\.tar'
require_pattern makefiles/build.mk '\$\(REGISTRY\)/kube-ovn:\$\(RELEASE_TAG\)-almalinux10'
require_pattern makefiles/build.mk '\$\(REGISTRY\)/kube-ovn:\$\(DEBUG_TAG\)-almalinux10'
reject_pattern makefiles/build.mk 'almalinux10.*(LEGACY|dpdk)|((LEGACY|dpdk).*almalinux10)'

require_pattern Makefile '^KUBE_OVN_IMAGE_TAG_SUFFIX = \$\(shell echo \$\$\{KUBE_OVN_IMAGE_TAG_SUFFIX:-\}\)$'
require_pattern Makefile '^KUBE_OVN_VERSION = \$\(shell echo \$\$\{KUBE_OVN_VERSION:-\$\(VERSION\)\$\(KUBE_OVN_IMAGE_TAG_SUFFIX\)\}\)$'
require_pattern Makefile '^export VPC_NAT_VERSION = \$\(shell echo \$\$\{VPC_NAT_VERSION:-\$\(VERSION\)\}\)$'
require_pattern dist/images/install.sh '^VPC_NAT_VERSION="\$\{VPC_NAT_VERSION:-\$VERSION\}"$'
require_pattern dist/images/install.sh 'image: \$REGISTRY/\$VPC_NAT_IMAGE:\$VPC_NAT_VERSION'
require_pattern makefiles/kind.mk 'VPC_NAT_GW_IMG = \$\(REGISTRY\)/vpc-nat-gateway:\$\(RELEASE_TAG\)'
require_pattern makefiles/kind.mk 'kube-ovn:\$\(KUBE_OVN_VERSION\)'
require_pattern makefiles/kind.mk 'VERSION=\$\(KUBE_OVN_VERSION\)'
require_pattern makefiles/kind.mk 'KUBE_OVN_VERSION=\$\(DEBUG_TAG\)\$\(KUBE_OVN_IMAGE_TAG_SUFFIX\)'
reject_pattern makefiles/kind.mk 'kube-ovn:\$\(VERSION\)|@VERSION=\$\(DEBUG_TAG\)'

require_pattern .github/workflows/build-kube-ovn-base.yaml 'make base-\$\{\{ matrix\.arch \}\}-almalinux10'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'make base-tar-\$\{\{ matrix\.arch \}\}-almalinux10'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'image-\$\{\{ matrix\.arch \}\}-almalinux10-\$\{\{ matrix\.branch \}\}'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'image-\$\{\{ matrix\.arch \}\}-almalinux10\.tar'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'kubeovn/kube-ovn-base:\$TAG-almalinux10-amd64'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'kubeovn/kube-ovn-base:\$TAG-almalinux10-arm64'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'kubeovn/kube-ovn-base:\$TAG-almalinux10'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'kubeovn/kube-ovn-base:\$TAG-debug-almalinux10-amd64'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'kubeovn/kube-ovn-base:\$TAG-debug-almalinux10-arm64'
require_pattern .github/workflows/build-kube-ovn-base.yaml 'kubeovn/kube-ovn-base:\$TAG-debug-almalinux10'
reject_pattern .github/workflows/build-kube-ovn-base.yaml 'almalinux10.*(legacy|dpdk)|((legacy|dpdk).*almalinux10)'

require_pattern .github/workflows/build-x86-image.yaml 'dist/images/Dockerfile\.base-almalinux10'
require_pattern .github/workflows/build-x86-image.yaml 'make base-amd64-almalinux10'
require_pattern .github/workflows/build-x86-image.yaml 'make base-tar-amd64-almalinux10'
require_pattern .github/workflows/build-x86-image.yaml 'image-amd64-almalinux10\.tar'
require_pattern .github/workflows/build-x86-image.yaml 'kubeovn/kube-ovn-base:\$TAG-almalinux10-amd64'
require_pattern .github/workflows/build-x86-image.yaml 'kubeovn/kube-ovn-base:\$TAG-almalinux10'
require_pattern .github/workflows/build-x86-image.yaml 'kubeovn/kube-ovn-base:\$TAG-debug-almalinux10-amd64'
require_pattern .github/workflows/build-x86-image.yaml 'kubeovn/kube-ovn-base:\$TAG-debug-almalinux10'
require_pattern .github/workflows/build-x86-image.yaml 'needs\.build-kube-ovn-almalinux10-base\.outputs\.build-almalinux10-base'
require_pattern .github/workflows/build-x86-image.yaml 'make build-kube-ovn-almalinux10'
require_pattern .github/workflows/build-x86-image.yaml 'make image-kube-ovn-almalinux10'
reject_pattern .github/workflows/build-x86-image.yaml '^  KUBE_OVN_IMAGE_TAG_SUFFIX: -almalinux10$'
require_pattern .github/workflows/build-x86-image.yaml 'image-ref: docker\.io/kubeovn/kube-ovn-base:\$\{\{ env\.TAG \}\}-almalinux10'
require_pattern .github/workflows/build-x86-image.yaml 'tag_suffix: "-almalinux10"'
require_pattern .github/workflows/build-x86-image.yaml 'tag_suffix: "-debug-almalinux10"'
require_pattern .github/workflows/build-x86-image.yaml 'KUBE_OVN_VERSION=\$\(cat VERSION\)\$\{\{ matrix\.image\.tag_suffix \}\}'
require_pattern .github/workflows/build-x86-image.yaml 'kubeovn/kube-ovn:\$TAG-almalinux10-amd64'
require_pattern .github/workflows/build-x86-image.yaml 'kubeovn/kube-ovn:\$TAG-debug-almalinux10-amd64'
reject_pattern .github/workflows/build-x86-image.yaml 'almalinux10.*(legacy|dpdk)|((legacy|dpdk).*almalinux10)'
reject_pattern .github/workflows/build-x86-image.yaml 'almalinux10-(x86|arm)([^0-9A-Za-z_]|$)'
reject_pattern .github/workflows/build-x86-image.yaml 'tag_suffix: ""|name: default'

require_pattern .github/workflows/build-arm64-image.yaml 'make base-arm64-almalinux10'
require_pattern .github/workflows/build-arm64-image.yaml 'kubeovn/kube-ovn-base:\$TAG-almalinux10-arm64'
require_pattern .github/workflows/build-arm64-image.yaml 'kubeovn/kube-ovn-base:\$TAG-almalinux10'
require_pattern .github/workflows/build-arm64-image.yaml 'kubeovn/kube-ovn-base:\$TAG-debug-almalinux10-arm64'
require_pattern .github/workflows/build-arm64-image.yaml 'kubeovn/kube-ovn-base:\$TAG-debug-almalinux10'
require_pattern .github/workflows/build-arm64-image.yaml 'image-ref: docker\.io/kubeovn/kube-ovn-base:\$\{\{ env\.TAG \}\}-almalinux10'
require_pattern .github/workflows/build-arm64-image.yaml 'make build-kube-ovn-arm64-almalinux10'
require_pattern .github/workflows/build-arm64-image.yaml 'make image-kube-ovn-arm64-almalinux10'
require_pattern .github/workflows/build-arm64-image.yaml 'kubeovn/kube-ovn:\$TAG-almalinux10-arm64'
require_pattern .github/workflows/build-arm64-image.yaml 'kubeovn/kube-ovn:\$TAG-debug-almalinux10-arm64'
reject_pattern .github/workflows/build-arm64-image.yaml 'almalinux10.*(legacy|dpdk)|((legacy|dpdk).*almalinux10)'
reject_pattern .github/workflows/build-arm64-image.yaml 'almalinux10-(x86|arm)([^0-9A-Za-z_]|$)'

require_pattern .github/workflows/publish.yaml 'kubeovn/kube-ovn:\$TAG-almalinux10-amd64'
require_pattern .github/workflows/publish.yaml 'kubeovn/kube-ovn:\$TAG-almalinux10-arm64'
require_pattern .github/workflows/publish.yaml 'kubeovn/kube-ovn:\$TAG-debug-almalinux10-amd64'
require_pattern .github/workflows/publish.yaml 'kubeovn/kube-ovn:\$TAG-debug-almalinux10-arm64'
require_pattern .github/workflows/publish.yaml 'docker manifest create kubeovn/kube-ovn:\$TAG-almalinux10 kubeovn/kube-ovn:\$TAG-almalinux10-amd64 kubeovn/kube-ovn:\$TAG-almalinux10-arm64'
require_pattern .github/workflows/publish.yaml 'docker manifest create kubeovn/kube-ovn:\$TAG-debug-almalinux10 kubeovn/kube-ovn:\$TAG-debug-almalinux10-amd64 kubeovn/kube-ovn:\$TAG-debug-almalinux10-arm64'
require_pattern .github/workflows/publish.yaml 'docker manifest push kubeovn/kube-ovn:\$TAG-almalinux10'
require_pattern .github/workflows/publish.yaml 'docker manifest push kubeovn/kube-ovn:\$TAG-debug-almalinux10'
reject_pattern .github/workflows/publish.yaml 'almalinux10-(x86|arm)([^0-9A-Za-z_]|$)'

require_pattern hack/release.sh 'kubeovn/kube-ovn:\$\{VERSION\}-almalinux10-amd64'
require_pattern hack/release.sh 'kubeovn/kube-ovn:\$\{VERSION\}-almalinux10-arm64'
require_pattern hack/release.sh 'kubeovn/kube-ovn:\$\{VERSION\}-debug-almalinux10-amd64'
require_pattern hack/release.sh 'kubeovn/kube-ovn:\$\{VERSION\}-debug-almalinux10-arm64'
require_pattern hack/release.sh 'docker manifest create kubeovn/kube-ovn:\$\{VERSION\}-almalinux10 kubeovn/kube-ovn:\$\{VERSION\}-almalinux10-amd64 kubeovn/kube-ovn:\$\{VERSION\}-almalinux10-arm64'
require_pattern hack/release.sh 'docker manifest create kubeovn/kube-ovn:\$\{VERSION\}-debug-almalinux10 kubeovn/kube-ovn:\$\{VERSION\}-debug-almalinux10-amd64 kubeovn/kube-ovn:\$\{VERSION\}-debug-almalinux10-arm64'
require_pattern hack/release.sh 'docker manifest push kubeovn/kube-ovn:\$\{VERSION\}-almalinux10'
require_pattern hack/release.sh 'docker manifest push kubeovn/kube-ovn:\$\{VERSION\}-debug-almalinux10'
reject_pattern hack/release.sh 'almalinux10-(x86|arm)([^0-9A-Za-z_]|$)'

require_pattern dist/images/go-deps/download-go-deps.sh 'download_archive\(\)'
require_pattern dist/images/go-deps/download-go-deps.sh '--retry-all-errors'
require_pattern dist/images/go-deps/download-go-deps.sh '--connect-timeout'
require_pattern dist/images/go-deps/download-go-deps.sh '--max-time'
reject_pattern dist/images/go-deps/download-go-deps.sh 'curl .*\|[[:space:]]*\\?$'
