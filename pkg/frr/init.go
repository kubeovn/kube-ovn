package frr

import (
	"os"
	"path/filepath"
)

const daemonsConfig = `zebra=yes
bgpd=yes
bfdd=yes
vtysh_enable=yes
zebra_options=" -A 127.0.0.1 -s 90000000"
bgpd_options=" -A 127.0.0.1"
bfdd_options=" -A 127.0.0.1"
`

const vtyshConfig = `service integrated-vtysh-config
`

const reloadScript = `#!/bin/sh
FRR_DIR=${FRR_DIR:-/etc/frr}
RELOAD_PY=/usr/lib/frr/frr-reload.py
[ -f "$RELOAD_PY" ] || RELOAD_PY=$(command -v frr-reload.py)
REDACT='s/password .*/password (redacted)/'
while true; do
  sleep 1
  [ -f "$FRR_DIR/.kube-ovn-frr-apply" ] || continue
  want=$(cat "$FRR_DIR/.kube-ovn-frr-apply")
  have=$(cat "$FRR_DIR/.kube-ovn-frr-applied" 2>/dev/null)
  [ "$want" = "$have" ] && continue
  if ! python3 "$RELOAD_PY" --test "$FRR_DIR/frr.conf.desired" >/tmp/frr-test.log 2>&1; then
    { printf 'error %s test\n' "$want"; sed "$REDACT" /tmp/frr-test.log | tail -20; } > "$FRR_DIR/.kube-ovn-frr-result"
    sleep 5
    continue
  fi
  new_instances=$(grep '^router bgp .* vrf ' "$FRR_DIR/frr.conf.desired" 2>/dev/null | sort)
  old_instances=$(grep '^router bgp .* vrf ' "$FRR_DIR/frr.conf" 2>/dev/null | sort)
  added=$(printf '%s\n' "$new_instances" | while read -r line; do
    [ -n "$line" ] || continue
    printf '%s\n' "$old_instances" | grep -qxF -- "$line" || printf '%s\n' "$line"
  done)
  if [ -n "$added" ]; then
    cp "$FRR_DIR/frr.conf.desired" "$FRR_DIR/frr.conf"
    printf '%s\n' "$want" > "$FRR_DIR/.kube-ovn-frr-applied"
    printf 'ok %s restart\n' "$want" > "$FRR_DIR/.kube-ovn-frr-result"
    exit 0
  fi
  if python3 "$RELOAD_PY" --reload --overwrite "$FRR_DIR/frr.conf.desired" >/tmp/frr-reload.log 2>&1; then
    printf '%s\n' "$want" > "$FRR_DIR/.kube-ovn-frr-applied"
    printf 'ok %s\n' "$want" > "$FRR_DIR/.kube-ovn-frr-result"
  else
    { printf 'error %s reload\n' "$want"; sed "$REDACT" /tmp/frr-reload.log | tail -20; } > "$FRR_DIR/.kube-ovn-frr-result"
    sleep 5
  fi
done
`

func InitFrrDir(frrDir, nodeName string) error {
	if err := os.MkdirAll(frrDir, 0o755); err != nil {
		return err
	}
	files := map[string]string{
		"daemons":            daemonsConfig,
		"vtysh.conf":         vtyshConfig,
		"kube-ovn-reload.sh": reloadScript,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(frrDir, name), []byte(content), 0o600); err != nil {
			return err
		}
	}

	frrConf := filepath.Join(frrDir, "frr.conf")
	if _, err := os.Stat(frrConf); os.IsNotExist(err) {
		initial := Render(RenderInput{NodeName: nodeName})
		return os.WriteFile(frrConf, []byte(initial), 0o600)
	}
	return nil
}
