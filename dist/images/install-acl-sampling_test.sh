#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
INSTALL_SCRIPT="$SCRIPT_DIR/install.sh"

assert_equal() {
  local expected=$1
  local actual=$2
  if [ "$actual" != "$expected" ]; then
    printf 'expected %q, got %q\n' "$expected" "$actual" >&2
    exit 1
  fi
}

load_acl_sampling_config() {
  eval "$(sed -n \
    '/^ENABLE_ACL_SAMPLING=/,/^ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT=/p' \
    "$INSTALL_SCRIPT")"
}

extract_container() {
  local workload=$1
  local container=$2
  awk -v workload="$workload" -v container="$container" '
    function indentation(line, spaces) {
      spaces = line
      sub(/[^ ].*$/, "", spaces)
      return length(spaces)
    }

    /^---$/ {
      in_workload = 0
      capturing = 0
    }
    $0 == "  name: " workload {
      in_workload = 1
    }
    in_workload && $0 ~ "^ *- name: " container "$" {
      capturing = 1
      container_indent = indentation($0)
      print
      next
    }
    capturing {
      if ($0 != "" && indentation($0) <= container_indent) {
        exit
      }
      print
    }
  ' "$INSTALL_SCRIPT"
}

render_acl_sampling_args() {
  local workload=$1
  local container=$2
  local argument flag variable

  while IFS= read -r argument; do
    argument=${argument#*'- '}
    flag=${argument%%=*}
    variable=${argument#*=\$}
    if [[ $variable != ACL_SAMPLING_* && $variable != ENABLE_ACL_SAMPLING ]]; then
      printf 'unexpected ACL sampling argument %q\n' "$argument" >&2
      exit 1
    fi
    printf '%s=%s\n' "$flag" "${!variable}"
  done < <(
    extract_container "$workload" "$container" |
      grep -E -- '^[[:space:]]*- --.*acl-sampling.*=\$[A-Z_]+$'
  )
}

unset ENABLE_ACL_SAMPLING ACL_SAMPLING_SET_ID ACL_SAMPLING_LOCAL_GROUP_ID
unset ACL_SAMPLING_APP_ID_NEW ACL_SAMPLING_APP_ID_ESTABLISHED
unset ACL_SAMPLING_COLLECTOR_ID_ALLOW ACL_SAMPLING_COLLECTOR_ID_DEFAULT_DENY
unset ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT
load_acl_sampling_config

assert_equal false "$ENABLE_ACL_SAMPLING"
assert_equal 142 "$ACL_SAMPLING_SET_ID"
assert_equal 142 "$ACL_SAMPLING_LOCAL_GROUP_ID"
assert_equal 102 "$ACL_SAMPLING_APP_ID_NEW"
assert_equal 103 "$ACL_SAMPLING_APP_ID_ESTABLISHED"
assert_equal 1 "$ACL_SAMPLING_COLLECTOR_ID_ALLOW"
assert_equal 2 "$ACL_SAMPLING_COLLECTOR_ID_DEFAULT_DENY"
assert_equal 1 "$ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT"
assert_equal 100 "$ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT"

ENABLE_ACL_SAMPLING=true
ACL_SAMPLING_SET_ID=240
ACL_SAMPLING_LOCAL_GROUP_ID=241
ACL_SAMPLING_APP_ID_NEW=104
ACL_SAMPLING_APP_ID_ESTABLISHED=105
ACL_SAMPLING_COLLECTOR_ID_ALLOW=3
ACL_SAMPLING_COLLECTOR_ID_DEFAULT_DENY=4
ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT=0.5
ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT=75
load_acl_sampling_config

assert_equal true "$ENABLE_ACL_SAMPLING"
assert_equal 240 "$ACL_SAMPLING_SET_ID"
assert_equal 241 "$ACL_SAMPLING_LOCAL_GROUP_ID"
assert_equal 104 "$ACL_SAMPLING_APP_ID_NEW"
assert_equal 105 "$ACL_SAMPLING_APP_ID_ESTABLISHED"
assert_equal 3 "$ACL_SAMPLING_COLLECTOR_ID_ALLOW"
assert_equal 4 "$ACL_SAMPLING_COLLECTOR_ID_DEFAULT_DENY"
assert_equal 0.5 "$ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT"
assert_equal 75 "$ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT"

expected_controller_args=$(cat <<'EOF'
--enable-acl-sampling=true
--acl-sampling-set-id=240
--acl-sampling-app-id-new=104
--acl-sampling-app-id-established=105
--acl-sampling-collector-id-allow=3
--acl-sampling-collector-id-default-deny=4
--acl-sampling-allow-probability-percent=0.5
--acl-sampling-default-deny-probability-percent=75
EOF
)
actual_controller_args=$(render_acl_sampling_args kube-ovn-controller kube-ovn-controller)
assert_equal "$expected_controller_args" "$actual_controller_args"

expected_cni_args=$(cat <<'EOF'
--enable-acl-sampling=true
--acl-sampling-set-id=240
--acl-sampling-local-group-id=241
EOF
)
actual_cni_args=$(render_acl_sampling_args kube-ovn-cni cni-server)
assert_equal "$expected_cni_args" "$actual_cni_args"

echo 'install.sh ACL sampling tests passed'
