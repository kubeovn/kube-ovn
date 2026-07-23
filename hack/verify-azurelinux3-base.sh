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
reject_file dist/images/Dockerfile.base-azurelinux3

require_pattern dist/images/Dockerfile.base 'FROM mcr\.microsoft\.com/azurelinux/base/core:3\.0 AS ovs-builder'
require_pattern dist/images/Dockerfile.base 'FROM mcr\.microsoft\.com/azurelinux/base/core:3\.0 AS ndisc6-builder'
require_pattern dist/images/Dockerfile.base 'FROM mcr\.microsoft\.com/azurelinux/base/core:3\.0 AS runtime'
require_pattern dist/images/Dockerfile.base 'LABEL "org.opencontainers.image.ref.name"="azurelinux3"'
require_pattern dist/images/Dockerfile.base 'LABEL "org.opencontainers.image.version"="3\.0"'
require_pattern dist/images/Dockerfile.base "ARG SRC_DIR='/usr/src/'"
require_pattern dist/images/Dockerfile.base 'tdnf -y install'
require_pattern dist/images/Dockerfile.base 'tdnf clean all'
require_pattern dist/images/Dockerfile.base 'autoconf automake binutils bzip2 ca-certificates checkpolicy'
require_pattern dist/images/Dockerfile.base 'gcc gcc-c\+\+ git glibc-devel'
require_pattern dist/images/Dockerfile.base 'kernel-headers pkgconf-pkg-config'
require_pattern dist/images/Dockerfile.base 'binutils bzip2 ca-certificates curl gawk gcc glibc-devel kernel-headers make tar'
require_pattern dist/images/Dockerfile.base 'rpm-build'
require_pattern dist/images/Dockerfile.base 'make rpm-fedora'
require_pattern dist/images/Dockerfile.base '--without check --without dpdk --without afxdp --without usdt'
require_pattern dist/images/Dockerfile.base 'ovs_commit=\$\(git rev-parse --short=12 HEAD\)'
require_pattern dist/images/Dockerfile.base 'ovn_commit=\$\(git rev-parse --short=12 HEAD\)'
require_pattern dist/images/Dockerfile.base 'AC_INIT'
reject_pattern dist/images/Dockerfile.base '\+g\$\{ovs_commit\}|\+g\$\{ovn_commit\}'
require_pattern dist/images/Dockerfile.base 'conntrack-tools'
require_pattern dist/images/Dockerfile.base 'coreutils'
require_pattern dist/images/Dockerfile.base 'gawk grep'
require_pattern dist/images/Dockerfile.base 'ipset iptables'
require_pattern dist/images/Dockerfile.base 'iputils'
require_pattern dist/images/Dockerfile.base 'ipvsadm'
require_pattern dist/images/Dockerfile.base 'procps-ng sed'
require_pattern dist/images/Dockerfile.base 'NDISC6_VERSION=1\.0\.8'
require_pattern dist/images/Dockerfile.base 'www\.remlab\.net/files/ndisc6/ndisc6-\$\{NDISC6_VERSION\}\.tar\.bz2'
require_pattern dist/images/Dockerfile.base "CPP='gcc -E' ./configure --disable-nls"
require_pattern dist/images/Dockerfile.base 'ndisc6'
require_pattern dist/images/Dockerfile.base 'strongswan'
require_pattern dist/images/Dockerfile.base 'initscripts'
require_pattern dist/images/Dockerfile.base 'ln -s /usr/sbin/strongswan /usr/sbin/ipsec'
require_pattern dist/images/Dockerfile.base 'ln -s /etc/strongswan/ipsec\.d /etc/ipsec\.d'
require_pattern dist/images/Dockerfile.base 'touch /etc/strongswan\.d/ovs\.conf'
require_pattern dist/images/Dockerfile.base '/etc/init\.d/openvswitch-ipsec'
require_pattern dist/images/Dockerfile.base 'ovs-monitor-ipsec'
require_pattern dist/images/Dockerfile.base 'fix-netdev-get-ifindex-type\.patch'
require_pattern dist/images/Dockerfile.base 'command -v ovs-vswitchd'
require_pattern dist/images/Dockerfile.base '^ARG DEBUG=false$'
require_pattern dist/images/Dockerfile.base 'azurelinux-official-base-debuginfo'
require_pattern dist/images/Dockerfile.base '--enablerepo=azurelinux-official-base-debuginfo install gdb valgrind'
require_pattern dist/images/Dockerfile.base 'gdb valgrind'
require_pattern dist/images/Dockerfile.base 'command -v xtables-legacy-multi'
require_pattern dist/images/Dockerfile.base 'if \[ "\$DEBUG" != "true" \]; then'
require_pattern dist/images/Dockerfile.base 'setcap CAP_NET_BIND_SERVICE\+eip \$\(readlink -f \$\(command -v ovsdb-server\)\)'
require_pattern dist/images/Dockerfile.base 'setcap CAP_NET_ADMIN,CAP_NET_BIND_SERVICE,CAP_SYS_ADMIN\+eip \$\(readlink -f \$\(command -v ovs-vswitchd\)\)'
reject_pattern dist/images/Dockerfile.base 'FROM ubuntu:24\.04|FROM almalinux:10|apt-get|[[:space:]]apt[[:space:]]|[[:space:]]dnf[[:space:]]|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy'
reject_pattern dist/images/Dockerfile.base 'rpmbuild -bb --with legacy|baseos-source|crb|epel-release|iptables-legacy-\[0-9\]\*\.rpm|iptables-legacy-libs-\[0-9\]\*\.rpm|iptables-nft|curl-minimal|unbound-libs|initscripts-service'

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
reject_pattern makefiles/build.mk 'Dockerfile\.base-azurelinux3|KUBE_OVN_IMAGE_TAG_SUFFIX|KUBE_OVN_VERSION|-azurelinux3|PROXY_BUILD_ARGS|HTTP_PROXY|HTTPS_PROXY|NO_PROXY|http_proxy|https_proxy|no_proxy'

reject_pattern Makefile 'KUBE_OVN_IMAGE_TAG_SUFFIX|KUBE_OVN_VERSION|VPC_NAT_VERSION'
require_pattern Makefile 'HELM_WAIT_TIMEOUT \?= 10m'
require_pattern Makefile 'helm install kubeovn \./charts/kube-ovn --wait --timeout=\$\(HELM_WAIT_TIMEOUT\)'
require_pattern Makefile 'helm upgrade kubeovn \./charts/kube-ovn --wait --timeout=\$\(HELM_WAIT_TIMEOUT\)'
require_pattern Makefile 'helm install kubeovn \./charts/kube-ovn-v2 --wait --timeout=\$\(HELM_WAIT_TIMEOUT\)'
require_pattern Makefile 'helm upgrade kubeovn \./charts/kube-ovn-v2 --wait --timeout=\$\(HELM_WAIT_TIMEOUT\)'
reject_pattern dist/images/install.sh 'VPC_NAT_VERSION'
reject_pattern makefiles/kind.mk 'KUBE_OVN_IMAGE_TAG_SUFFIX|KUBE_OVN_VERSION'
require_pattern dist/images/install.sh '/usr/share/openvswitch/scripts/ovs-ctl load-kmod \|\| true'
require_pattern charts/kube-ovn/templates/ovsovn-ds.yaml '/usr/share/openvswitch/scripts/ovs-ctl load-kmod \|\| true'
require_pattern charts/kube-ovn-v2/templates/ovs-ovn/ovs-ovn-daemonset.yaml '/usr/share/openvswitch/scripts/ovs-ctl load-kmod \|\| true'
require_pattern dist/images/install.sh 'ln -sf /bin/true /usr/local/sbin/modprobe'
require_pattern charts/kube-ovn/templates/ovsovn-ds.yaml 'ln -sf /bin/true /usr/local/sbin/modprobe'
require_pattern charts/kube-ovn-v2/templates/ovs-ovn/ovs-ovn-daemonset.yaml 'ln -sf /bin/true /usr/local/sbin/modprobe'
reject_pattern dist/images/install.sh 'SYS_MODULE'
reject_pattern charts/kube-ovn/templates/ovsovn-ds.yaml 'SYS_MODULE'
reject_pattern charts/kube-ovn-v2/templates/ovs-ovn/ovs-ovn-daemonset.yaml 'SYS_MODULE'

require_pattern test/e2e/metallb/e2e_test.go 'arping -c 5 -w 10'
reject_pattern test/e2e/metallb/e2e_test.go 'arping -c 5 -W 2'

reject_pattern .github/workflows/build-x86-image.yaml 'azurelinux3|KUBE_OVN_IMAGE_TAG_SUFFIX|tag_suffix'
reject_pattern .github/workflows/build-arm64-image.yaml 'azurelinux3|KUBE_OVN_IMAGE_TAG_SUFFIX'
reject_pattern .github/workflows/build-kube-ovn-base.yaml 'azurelinux3'
reject_pattern .github/workflows/publish.yaml 'azurelinux3'
reject_pattern hack/release.sh 'azurelinux3'

require_pattern dist/images/go-deps/download-go-deps.sh 'download_archive\(\)'
require_pattern dist/images/go-deps/download-go-deps.sh '--retry-all-errors'
require_pattern dist/images/go-deps/download-go-deps.sh '--connect-timeout'
require_pattern dist/images/go-deps/download-go-deps.sh '--max-time'

require_pattern dist/images/start-ovs.sh 'set -Eeuo pipefail'
require_pattern dist/images/start-ovs.sh 'start-ovs\.sh failed at line'
require_pattern dist/images/start-ovs.sh 'ovs diagnostics'
require_pattern dist/images/start-ovs.sh 'run_or_log /usr/share/openvswitch/scripts/ovs-ctl restart --no-ovsdb-server'
require_pattern dist/images/start-ovs.sh 'set \+e'

require_pattern dist/images/iptables-wrapper-installer.sh 'alternatives --set ip6tables'
require_pattern dist/images/iptables-wrapper-installer.sh 'alternatives --set iptables .* > /dev/null 2>&1 \|\| true'
require_pattern dist/images/iptables-wrapper-installer.sh 'alternatives --set ip6tables .* > /dev/null 2>&1 \|\| true'
require_pattern dist/images/iptables-wrapper-installer.sh '--install /usr/sbin/ip6tables ip6tables /usr/sbin/iptables-wrapper 100'
