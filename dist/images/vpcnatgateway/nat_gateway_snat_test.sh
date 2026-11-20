#!/usr/bin/env bash
set -Eeuo pipefail

# Focused regression test for SNAT rule deletion. It uses fake iptables and
# conntrack commands so it can run without CAP_NET_ADMIN.

script="$(cd "$(dirname "$0")" && pwd)/nat-gateway.sh"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

# Load definitions without executing the command dispatcher at the end.
eval "$(sed '/^opt=\$1$/,$d' "$script")"
set +e

cmd_log="$tmp_dir/cmd.log"

fake_iptables_save() {
  printf -- '-A SNAT_FILTER -j SHARED_SNAT\n'
  printf -- '-A SHARED_SNAT -s 10.0.1.0/24 -o net1 -j SNAT --to-source 172.18.0.10 --random-fully\n'
  printf -- '-A SHARED_SNAT -s 10.0.2.0/24 -o net1 -j SNAT --to-source 172.18.0.10 --random-fully\n'
}
fake_iptables() {
  printf 'iptables %s\n' "$*" >>"$cmd_log"
}
conntrack() {
  printf 'conntrack %s\n' "$*" >>"$cmd_log"
}
iptables_save_cmd=fake_iptables_save
iptables_cmd=fake_iptables

del_snat 172.18.0.10,10.0.1.0/24 >/dev/null

set -e
grep -q -- 'iptables -t nat -D SHARED_SNAT -s 10.0.1.0/24 -o net1 -j SNAT --to-source 172.18.0.10 --random-fully' "$cmd_log"
grep -q -- 'conntrack -D --orig-src 10.0.1.0/24 --reply-dst 172.18.0.10' "$cmd_log"
if grep -q -- '10.0.2.0/24' "$cmd_log"; then
  echo 'FAIL: del_snat touched the rule or flows of another CIDR' >&2
  exit 1
fi

echo 'PASS: del_snat removes the rule and flushes conntrack entries it translated'
