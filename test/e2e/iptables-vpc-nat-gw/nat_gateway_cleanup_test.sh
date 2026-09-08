#!/usr/bin/env bash
set -Eeuo pipefail

# Focused regression test for affinity-object cleanup. It uses a fake nft command
# so it can run without CAP_NET_ADMIN or a live nftables table.

repo_root="$(cd "$(dirname "$0")/../../.." && pwd)"
script="$repo_root/dist/images/vpcnatgateway/nat-gateway.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

# Load definitions without executing the command dispatcher at the end.
eval "$(sed '/^opt=\$1$/,$d' "$script")"

NFT_TABLE="kube-ovn"
nft_log="$tmp_dir/nft.log"

nft() {
  if [[ "$1" == "list" && "$2" == "table" ]]; then
    printf 'table ip kube-ovn {\n'
    printf ' chain ep-abc123456789-deadbeef { }\n'
    printf ' chain ep-abc123456789-keepbeef { }\n'
    printf ' set aff-abc123456789-deadbeef { type ipv4_addr; }\n'
    printf ' set aff-abc123456789-keepbeef { type ipv4_addr; }\n'
    printf '}\n'
    printf 'list table %s\n' "$*" >>"$nft_log"
    return 0
  fi
  printf 'nft %s\n' "$*" >>"$nft_log"
  if [[ "$1" == "-f" ]]; then
    cat >>"$nft_log"
  fi
}

cleanup_nft_affinity_objects abc123456789 keepbeef

! grep -q 'list chains ip' "$nft_log"
! grep -q 'list sets ip' "$nft_log"
grep -q 'list table ip kube-ovn' "$nft_log"
grep -q 'delete chain ip kube-ovn ep-abc123456789-deadbeef' "$nft_log"
grep -q 'delete set ip kube-ovn aff-abc123456789-deadbeef' "$nft_log"
! grep -q 'delete chain ip kube-ovn ep-abc123456789-keepbeef' "$nft_log"
! grep -q 'delete set ip kube-ovn aff-abc123456789-keepbeef' "$nft_log"

echo 'PASS: nft affinity cleanup enumerates the table and removes stale objects'
