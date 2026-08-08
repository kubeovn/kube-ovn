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

assert_script_contains() {
  local expected=$1
  if ! grep -Fq -- "$expected" "$INSTALL_SCRIPT"; then
    printf 'install.sh does not contain %q\n' "$expected" >&2
    exit 1
  fi
}

load_acl_sampling_config() {
  eval "$(sed -n \
    '/^ENABLE_ACL_SAMPLING=/,/^ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT=/p' \
    "$INSTALL_SCRIPT")"
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
ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT=100
ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT=25
load_acl_sampling_config

assert_equal true "$ENABLE_ACL_SAMPLING"
assert_equal 240 "$ACL_SAMPLING_SET_ID"
assert_equal 241 "$ACL_SAMPLING_LOCAL_GROUP_ID"
assert_equal 100 "$ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT"
assert_equal 25 "$ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT"

for argument in \
  "--enable-acl-sampling=\$ENABLE_ACL_SAMPLING" \
  "--acl-sampling-set-id=\$ACL_SAMPLING_SET_ID" \
  "--acl-sampling-local-group-id=\$ACL_SAMPLING_LOCAL_GROUP_ID" \
  "--acl-sampling-app-id-new=\$ACL_SAMPLING_APP_ID_NEW" \
  "--acl-sampling-app-id-established=\$ACL_SAMPLING_APP_ID_ESTABLISHED" \
  "--acl-sampling-collector-id-allow=\$ACL_SAMPLING_COLLECTOR_ID_ALLOW" \
  "--acl-sampling-collector-id-default-deny=\$ACL_SAMPLING_COLLECTOR_ID_DEFAULT_DENY" \
  "--acl-sampling-allow-probability-percent=\$ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT" \
  "--acl-sampling-default-deny-probability-percent=\$ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT"
do
  assert_script_contains "$argument"
done

echo 'install.sh ACL sampling tests passed'
