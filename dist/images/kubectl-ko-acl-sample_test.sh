#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
TEST_DIR=$(mktemp -d)
trap 'rm -rf "$TEST_DIR"' EXIT

FAKE_BIN="$TEST_DIR/bin"
FAKE_LOG="$TEST_DIR/kubectl.log"
mkdir -p "$FAKE_BIN"

cat > "$FAKE_BIN/kubectl" <<'FAKE'
#!/usr/bin/env bash
set -euo pipefail

printf '%s\n' "$*" >> "${FAKE_LOG:?}"

if [[ " $* " == *" get daemonset kube-ovn-cni "* ]]; then
  printf '%s\n' \
    "--enable-acl-sampling=${FAKE_ACL_ENABLED:-true}" \
    "--acl-sampling-local-group-id=5000"
  exit 0
fi

if [[ " $* " == *" get pods "*"--field-selector spec.nodeName=node-a"* ]]; then
  printf 'kube-ovn-cni-node-a\n'
  exit 0
fi

if [[ " $* " == *" get pod "*"ovn-nb-leader=true"* ]]; then
  printf 'ovn-central-0 1/1 Running 0 1m\n'
  exit 0
fi

if [[ " $* " == *" exec -i kube-ovn-cni-node-a "*" /kube-ovn/kube-ovn-acl-sample listen "* ]]; then
  printf '%s\n' '0x640abcde000000c8' '0x670abcde000000c9'
  exit 0
fi

if [[ " $* " == *" /kube-ovn/kube-ovn-acl-sample decode "* ]]; then
  cookie=${!#}
  printf 'schemaVersion: v1\nsample:\n  cookie: %s\n' "$cookie"
  exit 0
fi

echo "unexpected kubectl invocation: $*" >&2
exit 1
FAKE
chmod +x "$FAKE_BIN/kubectl"

assert_contains() {
  local output=$1
  local expected=$2
  if [[ "$output" != *"$expected"* ]]; then
    printf 'expected output to contain %q, got:\n%s\n' "$expected" "$output" >&2
    exit 1
  fi
}

decode_output=$(PATH="$FAKE_BIN:$PATH" FAKE_LOG="$FAKE_LOG" \
  bash "$SCRIPT_DIR/kubectl-ko" acl-sample decode 0x640abcde000000c8)
assert_contains "$decode_output" 'schemaVersion: v1'
assert_contains "$decode_output" 'cookie: 0x640abcde000000c8'

listen_output=$(PATH="$FAKE_BIN:$PATH" FAKE_LOG="$FAKE_LOG" \
  bash "$SCRIPT_DIR/kubectl-ko" acl-sample listen --node node-a)
assert_contains "$listen_output" 'cookie: 0x640abcde000000c8'
assert_contains "$listen_output" 'cookie: 0x670abcde000000c9'
if [[ $(grep -c '^---$' <<< "$listen_output") -ne 2 ]]; then
  printf 'expected two YAML document separators, got:\n%s\n' "$listen_output" >&2
  exit 1
fi

if PATH="$FAKE_BIN:$PATH" FAKE_LOG="$FAKE_LOG" FAKE_ACL_ENABLED=false \
  bash "$SCRIPT_DIR/kubectl-ko" acl-sample listen --node node-a \
  >"$TEST_DIR/disabled.out" 2>"$TEST_DIR/disabled.err"; then
  echo 'expected disabled ACL sampling listen to fail' >&2
  exit 1
fi
assert_contains "$(<"$TEST_DIR/disabled.err")" 'ACL sampling is not enabled'

assert_contains "$(<"$FAKE_LOG")" '--group-id=5000'
assert_contains "$(<"$FAKE_LOG")" '--ovn-nb-addr=unix:/var/run/ovn/ovnnb_db.sock'
assert_contains "$(<"$FAKE_LOG")" 'go-template={{range .spec.template.spec.containers}}{{if eq .name "cni-server"}}'

echo 'kubectl-ko ACL sample tests passed'
