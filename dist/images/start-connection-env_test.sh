#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

assert_contains() {
  local file="$1"
  local text="$2"
  grep -Fq -- "$text" "$file" || {
    echo "expected $file to contain: $text" >&2
    echo "actual:" >&2
    cat "$file" >&2
    exit 1
  }
}

test_start_controller_uses_explicit_ovn_addresses() {
  local tmp out
  tmp="$(mktemp -d)"
  out="$tmp/args"
  trap 'rm -rf "$tmp"' RETURN

  cp "$script_dir/start-controller.sh" "$tmp/"
  cat > "$tmp/kube-ovn-controller" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$OUT"
EOF
  chmod +x "$tmp/kube-ovn-controller"

  (
    cd "$tmp"
    OUT="$out" \
    ENABLE_SSL=false \
    OVN_NB_ADDR=tcp:nb.example.com:30641 \
    OVN_SB_ADDR=tcp:sb.example.com:30642 \
    bash ./start-controller.sh --extra-flag
  )

  assert_contains "$out" "--ovn-nb-addr=tcp:nb.example.com:30641"
  assert_contains "$out" "--ovn-sb-addr=tcp:sb.example.com:30642"
  assert_contains "$out" "--extra-flag"
}

test_start_ic_controller_uses_explicit_ovn_addresses() {
  local tmp out
  tmp="$(mktemp -d)"
  out="$tmp/args"
  trap 'rm -rf "$tmp"' RETURN

  cp "$script_dir/start-ic-controller.sh" "$tmp/"
  mkdir -p "$tmp/bin"
  cat > "$tmp/bin/ovn-nbctl" <<'EOF'
#!/usr/bin/env bash
printf '/var/run/ovn/ovn-nbctl.123.ctl\n'
EOF
  cat > "$tmp/kube-ovn-ic-controller" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$@" > "$OUT"
EOF
  chmod +x "$tmp/bin/ovn-nbctl" "$tmp/kube-ovn-ic-controller"

  (
    cd "$tmp"
    PATH="$tmp/bin:$PATH" \
    OUT="$out" \
    ENABLE_SSL=false \
    OVN_NB_ADDR=tcp:nb.example.com:30641 \
    OVN_SB_ADDR=tcp:sb.example.com:30642 \
    bash ./start-ic-controller.sh --extra-flag
  )

  assert_contains "$out" "--ovn-nb-addr=tcp:nb.example.com:30641"
  assert_contains "$out" "--ovn-sb-addr=tcp:sb.example.com:30642"
  assert_contains "$out" "--extra-flag"
}

test_upgrade_ovs_rolls_out_ovn_components() {
  local tmp out
  tmp="$(mktemp -d)"
  out="$tmp/args"
  trap 'rm -rf "$tmp"' RETURN

  cp "$script_dir/upgrade-ovs.sh" "$tmp/"
  mkdir -p "$tmp/bin"
  cat > "$tmp/bin/kubectl" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$OUT"
if [[ "$*" == *"jsonpath={.spec.updateStrategy.type}"* ]]; then
  printf 'RollingUpdate'
fi
EOF
  chmod +x "$tmp/bin/kubectl"

  (
    cd "$tmp"
    PATH="$tmp/bin:$PATH" \
    OUT="$out" \
    bash ./upgrade-ovs.sh
  )

  assert_contains "$out" "rollout status deploy ovn-central --timeout=120s"
  assert_contains "$out" "rollout status ds/ovs-ovn"
}

test_start_controller_uses_explicit_ovn_addresses
test_start_ic_controller_uses_explicit_ovn_addresses
test_upgrade_ovs_rolls_out_ovn_components
