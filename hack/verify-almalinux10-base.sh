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

reject_file() {
  local file="$1"
  [[ ! -e "$ROOT_DIR/$file" ]] || fail "unexpected $file"
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

require_file dist/images/Dockerfile.base
reject_file dist/images/Dockerfile.base-almalinux10

require_pattern dist/images/Dockerfile.base 'FROM almalinux:10 AS ovs-builder'
require_pattern dist/images/Dockerfile.base 'FROM almalinux:10 AS iptables-legacy-builder'
require_pattern dist/images/Dockerfile.base 'FROM almalinux:10 AS runtime'
require_pattern dist/images/Dockerfile.base 'LABEL "org.opencontainers.image.ref.name"="almalinux10"'
require_pattern dist/images/Dockerfile.base 'rpm-build'
require_pattern dist/images/Dockerfile.base 'rpmbuild -bb --with legacy'
require_pattern dist/images/Dockerfile.base "--define 'dist \\.el10_1'"
require_pattern dist/images/Dockerfile.base 'make rpm-fedora'
require_pattern dist/images/Dockerfile.base '--without check --without dpdk --without afxdp --without usdt'
require_pattern dist/images/Dockerfile.base 'ovs_commit=\$\(git rev-parse --short=12 HEAD\)'
require_pattern dist/images/Dockerfile.base 'ovn_commit=\$\(git rev-parse --short=12 HEAD\)'
require_pattern dist/images/Dockerfile.base 'AC_INIT'
reject_pattern dist/images/Dockerfile.base '\+g\$\{ovs_commit\}|\+g\$\{ovn_commit\}'
require_pattern dist/images/Dockerfile.base 'conntrack-tools'
require_pattern dist/images/Dockerfile.base 'iptables-nft'
require_pattern dist/images/Dockerfile.base 'iptables-legacy-\[0-9\]\*\.rpm'
require_pattern dist/images/Dockerfile.base 'iptables-legacy-libs-\[0-9\]\*\.rpm'
require_pattern dist/images/Dockerfile.base 'iputils'
require_pattern dist/images/Dockerfile.base 'ipvsadm'
require_pattern dist/images/Dockerfile.base 'ndisc6'
require_pattern dist/images/Dockerfile.base 'strongswan'
require_pattern dist/images/Dockerfile.base 'initscripts-service'
require_pattern dist/images/Dockerfile.base 'ln -s /usr/sbin/strongswan /usr/sbin/ipsec'
require_pattern dist/images/Dockerfile.base 'ln -s /etc/strongswan/ipsec\.d /etc/ipsec\.d'
require_pattern dist/images/Dockerfile.base 'touch /etc/strongswan\.d/ovs\.conf'
require_pattern dist/images/Dockerfile.base '/etc/init\.d/openvswitch-ipsec'
require_pattern dist/images/Dockerfile.base 'ovs-monitor-ipsec'
require_pattern dist/images/Dockerfile.base 'fix-netdev-get-ifindex-type\.patch'
require_pattern dist/images/Dockerfile.base 'command -v ovs-vswitchd'
require_pattern dist/images/Dockerfile.base '^ARG DEBUG=false$'
require_pattern dist/images/Dockerfile.base 'gdb valgrind'
require_pattern dist/images/Dockerfile.base 'command -v xtables-legacy-multi'
require_pattern dist/images/Dockerfile.base 'if \[ "\$DEBUG" != "true" \]; then'
require_pattern dist/images/Dockerfile.base 'setcap CAP_NET_BIND_SERVICE\+eip \$\(readlink -f \$\(command -v ovsdb-server\)\)'
require_pattern dist/images/Dockerfile.base 'setcap CAP_NET_ADMIN,CAP_NET_BIND_SERVICE,CAP_SYS_ADMIN\+eip \$\(readlink -f \$\(command -v ovs-vswitchd\)\)'
reject_pattern dist/images/Dockerfile.base 'FROM ubuntu:24\.04|apt-get|[[:space:]]apt[[:space:]]|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy'
reject_pattern dist/images/Dockerfile.base '/packages/iptables-libs-\[0-9\]\*\.rpm|/packages/iptables-nft-\[0-9\]\*\.rpm'

reject_pattern pkg/daemon/controller_linux.go 'isLegacyIptablesAvailable|exec\.ErrNotFound|failed to check iptables-legacy availability'
reject_pattern pkg/daemon/controller_linux_test.go 'TestIsLegacyIptablesMode|TestIsLegacyIptablesAvailable|writeExecutable'

require_pattern makefiles/build.mk 'Dockerfile\.base dist/images/'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(RELEASE_TAG\)-amd64'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(LEGACY_TAG\)'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(DEBUG_TAG\)-amd64'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(RELEASE_TAG\)-arm64'
require_pattern makefiles/build.mk 'kube-ovn-base:\$\(DEBUG_TAG\)-arm64'
require_pattern makefiles/build.mk '--build-arg DEBUG=true -t \$\(REGISTRY\)/kube-ovn-base:\$\(DEBUG_TAG\)-amd64'
require_pattern makefiles/build.mk '--build-arg DEBUG=true -t \$\(REGISTRY\)/kube-ovn-base:\$\(DEBUG_TAG\)-arm64'
reject_pattern makefiles/build.mk 'Dockerfile\.base-almalinux10|KUBE_OVN_IMAGE_TAG_SUFFIX|KUBE_OVN_VERSION|-almalinux10|PROXY_BUILD_ARGS|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy'

reject_pattern Makefile 'KUBE_OVN_IMAGE_TAG_SUFFIX|KUBE_OVN_VERSION|VPC_NAT_VERSION'
reject_pattern dist/images/install.sh 'VPC_NAT_VERSION'
reject_pattern makefiles/kind.mk 'KUBE_OVN_IMAGE_TAG_SUFFIX|KUBE_OVN_VERSION'

require_pattern test/e2e/metallb/e2e_test.go 'arping -c 5 -w 10'
reject_pattern test/e2e/metallb/e2e_test.go 'arping -c 5 -W 2'

reject_pattern .github/workflows/build-x86-image.yaml 'almalinux10|KUBE_OVN_IMAGE_TAG_SUFFIX|tag_suffix'
reject_pattern .github/workflows/build-arm64-image.yaml 'almalinux10|KUBE_OVN_IMAGE_TAG_SUFFIX'
reject_pattern .github/workflows/build-kube-ovn-base.yaml 'almalinux10'
reject_pattern .github/workflows/publish.yaml 'almalinux10'
reject_pattern hack/release.sh 'almalinux10'

require_pattern dist/images/go-deps/download-go-deps.sh 'download_archive\(\)'
require_pattern dist/images/go-deps/download-go-deps.sh '--retry-all-errors'
require_pattern dist/images/go-deps/download-go-deps.sh '--connect-timeout'
require_pattern dist/images/go-deps/download-go-deps.sh '--max-time'
