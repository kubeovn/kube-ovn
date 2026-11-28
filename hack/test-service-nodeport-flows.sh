#!/usr/bin/env bash
set -euo pipefail

# Run against an isolated OVN NB/SB database with a real northd.
# Requires Go and OVN/OVS tools, including the ACL/LB state refresh fix
# present in the current branch-26.03 used by kube-ovn-base.
nodeport_repo=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
nodeport_bin=${OVN_TEST_BIN_DIR:-/usr/bin}
nodeport_schema=${OVN_TEST_SCHEMA_DIR:-/usr/share/ovn}
nodeport_run=$(mktemp -d "${TMPDIR:-/tmp}/nodeport-lb-XXXXXX")
export PATH="$nodeport_bin:$PATH"
export KUBE_OVN_NODEPORT_TEST_BIN="$nodeport_bin"
export KUBE_OVN_NODEPORT_TEST_DB="unix:$nodeport_run/db.sock"
nodeport_cleanup() {
  nodeport_status=$?
  ovs-appctl -t "$nodeport_run/northd.ctl" exit >/dev/null 2>&1 || true
  ovs-appctl -t "$nodeport_run/db.ctl" exit >/dev/null 2>&1 || true
  if (( nodeport_status == 0 )); then
    rm -rf "$nodeport_run"
  else
    echo "Failed OVN fixture retained at $nodeport_run" >&2
  fi
}
trap nodeport_cleanup EXIT
ovsdb-tool create "$nodeport_run/nb.db" "$nodeport_schema/ovn-nb.ovsschema"
ovsdb-tool create "$nodeport_run/sb.db" "$nodeport_schema/ovn-sb.ovsschema"
ovsdb-server --detach --no-chdir --pidfile="$nodeport_run/db.pid" --unixctl="$nodeport_run/db.ctl" --remote="punix:$nodeport_run/db.sock" "$nodeport_run/nb.db" "$nodeport_run/sb.db"
ovn-nbctl --db="$KUBE_OVN_NODEPORT_TEST_DB" init
ovn-sbctl --db="$KUBE_OVN_NODEPORT_TEST_DB" init
ovn-northd --detach --no-chdir --pidfile="$nodeport_run/northd.pid" --unixctl="$nodeport_run/northd.ctl" --log-file="$nodeport_run/northd.log" --ovnnb-db="$KUBE_OVN_NODEPORT_TEST_DB" --ovnsb-db="$KUBE_OVN_NODEPORT_TEST_DB"
ovn-nbctl --db="$KUBE_OVN_NODEPORT_TEST_DB" ls-add subnet -- lr-add vpc \
  -- lrp-add vpc rp 00:00:00:00:00:01 10.0.0.1/24 fd00::1/64 \
  -- lsp-add subnet router -- lsp-set-type router router -- lsp-set-options router router-port=rp -- lsp-set-addresses router router \
  -- lsp-add subnet client -- lsp-set-addresses client '00:00:00:00:00:10 10.0.0.10 fd00::10' \
  -- lsp-add subnet backend -- lsp-set-addresses backend '00:00:00:00:00:02 10.0.0.2 fd00::2' \
  -- lsp-add subnet localnet.subnet -- lsp-set-type localnet.subnet localnet -- lsp-set-addresses localnet.subnet unknown -- lsp-set-options localnet.subnet network_name=physnet
if [[ -n ${NODEPORT_LB_TEST_BINARY:-} ]]; then
  "$NODEPORT_LB_TEST_BINARY" -test.run '^TestServiceNodePortLogicalFlows$' -test.count=1 -test.v
else
  cd "$nodeport_repo"
  go test -p 1 ./pkg/controller -run '^TestServiceNodePortLogicalFlows$' -count=1 -v
fi
