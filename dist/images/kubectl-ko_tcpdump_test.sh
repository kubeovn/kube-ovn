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
command_line=" $* "

if [[ "$command_line" == *" config view "* ]]; then
  printf 'ns1\n'
  exit 0
fi

if [[ "$command_line" == *" get vmi "* ]]; then
  if [[ "${FAKE_MODE:-}" == rbac ]]; then
    echo 'Error from server (Forbidden): virtualmachineinstances is forbidden' >&2
    exit 1
  fi
  if [[ "${FAKE_MODE:-}" == vm ]]; then
    if [[ "$command_line" == *'ownerReferences[?(@.kind == "VirtualMachine")].kind'* ]]; then
      printf 'VirtualMachine\n'
    elif [[ "$command_line" == *'.status.migrationState.sourcePod'* ]]; then
      [[ "${FAKE_MIGRATION:-}" == source ]] && printf 'virt-launcher-source\n'
    elif [[ "$command_line" == *'.status.migrationState.targetPod'* ]]; then
      [[ "${FAKE_MIGRATION:-}" == target ]] && printf 'virt-launcher-target\n'
    elif [[ "$command_line" == *'.status.nodeName'* ]]; then
      printf 'node-a\n'
    fi
  fi
  exit 0
fi

if [[ "$command_line" == *" get pod -n ns2 -l vm.kubevirt.io/name=vm1 "* ]]; then
  printf 'virt-launcher-running\n'
  exit 0
fi

if [[ "$command_line" == *" get pod virt-launcher-"* ]]; then
  if [[ "$command_line" == *'ownerReferences[?(@.kind == "VirtualMachineInstance")].kind'* ]]; then
    printf 'VirtualMachineInstance\n'
  elif [[ "$command_line" == *'ownerReferences[?(@.kind == "VirtualMachineInstance")].name'* ]]; then
    printf 'vm1\n'
  elif [[ "$command_line" == *'.spec.nodeName'* ]]; then
    printf 'node-a\n'
  elif [[ "$command_line" == *'.spec.hostNetwork'* ]]; then
    printf 'false\n'
  elif [[ "$command_line" == *'pod_nic_type'* ]]; then
    printf 'veth-pair\n'
  fi
  exit 0
fi

if [[ "$command_line" == *" get pod pod1 "* ]]; then
  if [[ "$command_line" == *'ownerReferences[?(@.kind == "VirtualMachineInstance")].kind'* ]]; then
    [[ "${FAKE_MODE:-}" == pod ]] || exit 0
  elif [[ "$command_line" == *'.spec.nodeName'* ]]; then
    printf 'node-a\n'
  elif [[ "$command_line" == *'.spec.hostNetwork'* ]]; then
    printf 'false\n'
  elif [[ "$command_line" == *'pod_nic_type'* ]]; then
    printf 'internal-port\n'
  fi
  exit 0
fi

if [[ "$command_line" == *" get pod -n kube-system -l app=ovs "* ]]; then
  printf 'ovs-a\n'
  exit 0
fi

if [[ "$command_line" == *" get pod -n kube-system -l app=kube-ovn-cni "* ]]; then
  printf 'cni-a\n'
  exit 0
fi

if [[ "$command_line" == *" exec ovs-a "* ]]; then
  if [[ "$command_line" == *'find interface'* ]]; then
    if [[ "${FAKE_MODE:-}" == vm ]]; then printf 'vm1.ns2_h\n'; else printf 'pod1.ns2_h\n'; fi
  elif [[ "$command_line" == *'get interface'* ]]; then
    printf '"/var/run/netns/cni-test"\n'
  fi
  exit 0
fi

if [[ "$command_line" == *" exec cni-a "* ]]; then
  printf 'PACKETS\n'
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
    printf 'expected %q in:\n%s\n' "$expected" "$output" >&2
    exit 1
  fi
}

run_dump() {
  local mode=$1 migration=$2 target=$3 stderr=$4
  : > "$FAKE_LOG"
  PATH="$FAKE_BIN:$PATH" FAKE_LOG="$FAKE_LOG" FAKE_MODE="$mode" FAKE_MIGRATION="$migration" \
    bash "$SCRIPT_DIR/kubectl-ko" tcpdump "$target" -c 1 >"$TEST_DIR/stdout" 2>"$stderr"
  assert_contains "$(<"$TEST_DIR/stdout")" 'PACKETS'
}

run_dump vm none ns2/vm1 "$TEST_DIR/vm.err"
assert_contains "$(<"$FAKE_LOG")" 'get vmi vm1 -n ns2 --ignore-not-found'
assert_contains "$(<"$FAKE_LOG")" 'status.phase=Running'
assert_contains "$(<"$FAKE_LOG")" 'get interface vm1.ns2_h'
[[ $(<"$TEST_DIR/stdout") == PACKETS ]] || { echo 'unexpected diagnostics on stdout' >&2; exit 1; }

run_dump vm source ns2/vm1 "$TEST_DIR/source.err"
assert_contains "$(<"$TEST_DIR/source.err")" 'Migration in progress'
assert_contains "$(<"$FAKE_LOG")" 'get pod virt-launcher-source -n ns2'
[[ $(<"$TEST_DIR/stdout") == PACKETS ]] || { echo 'migration diagnostics leaked to stdout' >&2; exit 1; }

run_dump vm target ns2/vm1 "$TEST_DIR/target.err"
assert_contains "$(<"$FAKE_LOG")" 'get pod virt-launcher-target -n ns2'

run_dump pod none ns2/pod1 "$TEST_DIR/pod.err"
assert_contains "$(<"$FAKE_LOG")" 'get pod pod1 -n ns2'
assert_contains "$(<"$FAKE_LOG")" 'nsenter --net=/var/run/netns/cni-test tcpdump -nn -i pod1.ns2_h'

: > "$FAKE_LOG"
if PATH="$FAKE_BIN:$PATH" FAKE_LOG="$FAKE_LOG" FAKE_MODE=rbac \
  bash "$SCRIPT_DIR/kubectl-ko" tcpdump ns2/vm1 >"$TEST_DIR/rbac.out" 2>"$TEST_DIR/rbac.err"; then
  echo 'expected an RBAC error to fail tcpdump' >&2
  exit 1
fi
assert_contains "$(<"$TEST_DIR/rbac.err")" 'Forbidden'

echo 'kubectl-ko tcpdump tests passed'
