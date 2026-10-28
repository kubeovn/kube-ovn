#!/bin/bash

set -u -e

exit_with_error(){
  echo "$1"
  exit 1
}

# Copy to a temp file on the same filesystem, then rename over the
# destination. A plain `cp -f` truncates and rewrites the destination
# inode in place, so kubelet may exec a partially written binary
# during the copy window.
install_binary(){
  local src=$1 dst=$2
  local tmp="${dst}.tmp.$$"
  rm -f "${dst}".tmp.*
  if ! cp -f "$src" "$tmp" || ! mv -f "$tmp" "$dst"; then
    rm -f "$tmp"
    exit_with_error "Failed to install $src to $dst"
  fi
}

installCniPlugins=true
daemonArgs=()
for arg in "$@"; do
  case "$arg" in
    --install-cni-plugins=*)
      installCniPlugins="${arg#*=}"
      ;;
    *)
      daemonArgs+=("$arg")
      ;;
  esac
done

case "$installCniPlugins" in
  true|false)
    ;;
  *)
    exit_with_error "--install-cni-plugins must be true or false"
    ;;
esac

mkdir -p /usr/local/bin
install_binary /kube-ovn/kubectl-ko /usr/local/bin/kubectl-ko
chmod +x /usr/local/bin/kubectl-ko

for ip in $(echo "${POD_IPS}" | tr ',' ' '); do
  if [[ $ip == *:* ]]; then
    echo "IPv6 node IP $ip detected, enable IPv6 forwarding"
    echo 1 > /proc/sys/net/ipv6/conf/all/forwarding;
  else
    echo "IPv4 node IP $ip detected, enable IPv4 forwarding and disable ipv4 rp_filter"
    echo 1 > /proc/sys/net/ipv4/ip_forward;
    echo 0 > /proc/sys/net/ipv4/conf/all/rp_filter;
  fi
done

CNI_BIN_SRC=/kube-ovn/kube-ovn
CNI_BIN_DST=/opt/cni/bin/kube-ovn

bash /kube-ovn/install-cni-bundle.sh "$CNI_BIN_SRC" "$CNI_BIN_DST"

if [[ "$installCniPlugins" == true ]]; then
  LOOPBACK_BIN_SRC=/loopback
  LOOPBACK_BIN_DST=/opt/cni/bin/loopback

  PORTMAP_BIN_SRC=/portmap
  PORTMAP_BIN_DST=/opt/cni/bin/portmap

  MACVLAN_BIN_SRC=/macvlan
  MACVLAN_BIN_DST=/opt/cni/bin/macvlan

  IPVLAN_BIN_SRC=/ipvlan
  IPVLAN_BIN_DST=/opt/cni/bin/ipvlan

  install_binary "$LOOPBACK_BIN_SRC" "$LOOPBACK_BIN_DST"
  install_binary "$PORTMAP_BIN_SRC" "$PORTMAP_BIN_DST"
  install_binary "$MACVLAN_BIN_SRC" "$MACVLAN_BIN_DST"
  install_binary "$IPVLAN_BIN_SRC" "$IPVLAN_BIN_DST"
fi

./kube-ovn-daemon --install-cni-config "${daemonArgs[@]}" || exit_with_error "Failed to install cni config"
