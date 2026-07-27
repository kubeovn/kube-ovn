#!/usr/bin/env bash

set -euo pipefail

output="$(bfdd-control status)"

# bfdd-control exits successfully when the daemon is reachable even if the
# local BFD session table is empty, so inspect its output explicitly.
if grep -q '^There are 0 sessions:' <<< "${output}"; then
  printf '%s\n' "${output}" >&2
  exit 1
fi
