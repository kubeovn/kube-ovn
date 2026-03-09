#!/bin/bash

# Source the original script to get access to functions
# We use a subshell for running tests to avoid polluting the environment
SCRIPT_PATH="dist/images/upgrade-ovs.sh"

# Test cases for semver_compare
# expected, version1, version2
test_cases=(
  "0,1.15.1,1.15.1"
  "1,1.15.2,1.15.1"
  "-1,1.15.1,1.15.2"
  "1,2.0.0,1.15.1"
  "-1,1.15.1,2.0.0"
  "1,1.15.1,1.15"
  "0,1.15,1.15.0"
  "1,1.12.0,1.11.9"
  "1,v1.15.1,1.15.0"
  "0,v1.15.1,v1.15.1"
  "-1,1.15.1,v1.16.0"
  "1,1.15.1-alpha,1.15.0"
  "0,1.15.1-alpha,1.15.1" # Our current implementation cuts at '-'
)

failed=0

echo "Testing semver_compare..."
# Source script in a way that doesn't execute the main block
source "$SCRIPT_PATH"

for test_case in "${test_cases[@]}"; do
  IFS=',' read -r expected v1 v2 <<< "$test_case"
  result=$(semver_compare "$v1" "$v2")
  if [ "$result" -eq "$expected" ]; then
    echo "PASS: semver_compare $v1 $v2 => $result"
  else
    echo "FAIL: semver_compare $v1 $v2 => $result (expected $expected)"
    failed=$((failed + 1))
  fi
done

# Mocking environment for upgrade logic test
export POD_NAMESPACE="kube-system"
export CHART_NAME="kube-ovn"
export CHART_VERSION="1.17.0"

function test_upgrade_logic {
  local desc=$1
  local mock_ds_info=$2
  local expected_compatibility=$3
  local expected_exit_code=$4

  echo "Testing: $desc"

  # Run the detection part of the script in a subshell
  output=$(CHART_VERSION="$CHART_VERSION" \
    CHART_NAME="$CHART_NAME" \
    POD_NAMESPACE="$POD_NAMESPACE" \
    FORCE_UPGRADE="$FORCE_UPGRADE" \
    OVN_VERSION_COMPATIBILITY="$OVN_VERSION_COMPATIBILITY" \
    bash -c "
      source $SCRIPT_PATH
      function kubectl {
        if [[ \"\$*\" == *\"get ds ovs-ovn\"* ]]; then
          if [ -n \"$mock_ds_info\" ]; then
            echo -e \"$mock_ds_info\"
          else
            return 1
          fi
        else
          return 0
        fi
      }
      export -f kubectl
      detect_ovn_compatibility > /dev/null 2>&1
      echo \$OVN_VERSION_COMPATIBILITY
    ")
  
  result_compatibility=$(echo "$output" | tail -n 1)
  actual_exit_code=$?
  
  if [ "$actual_exit_code" -ne "$expected_exit_code" ]; then
      echo "FAIL: Exit code $actual_exit_code (expected $expected_exit_code)"
      failed=$((failed + 1))
      return
  fi

  if [ "$result_compatibility" == "$expected_compatibility" ]; then
    echo "PASS: Detected OVN_VERSION_COMPATIBILITY=$result_compatibility"
  else
    echo "FAIL: Detected OVN_VERSION_COMPATIBILITY=$result_compatibility (expected $expected_compatibility)"
    failed=$((failed + 1))
  fi
}

# Test cases for upgrade logic
# 1. Chart version hasn't changed
test_upgrade_logic "No chart version change" "kube-ovn-1.17.0\ndocker.io/kubeovn/kube-ovn:v1.16.0" "" 0

# 2. Fresh install (no DS found)
test_upgrade_logic "Fresh install (no DS)" "" "" 0

# 3. Valid upgrade 1.15.1 -> 1.17.0 (Compatibility 25.03)
test_upgrade_logic "Upgrade from 1.15.1" "kube-ovn-1.15.1\ndocker.io/kubeovn/kube-ovn:v1.15.1" "25.03" 0

# 4. Valid upgrade 1.13.5 -> 1.17.0 (Compatibility 24.03)
test_upgrade_logic "Upgrade from 1.13.5" "kube-ovn-1.13.5\ndocker.io/kubeovn/kube-ovn:v1.13.5" "24.03" 0

# 5. Invalid image version
test_upgrade_logic "Invalid image version" "kube-ovn-1.12.0\ndocker.io/kubeovn/kube-ovn:latest" "" 0

# 6. Compatibility switch 1.12.0 -> 22.12
test_upgrade_logic "Upgrade from 1.12.0" "kube-ovn-1.12.0\ndocker.io/kubeovn/kube-ovn:v1.12.0" "22.12" 0

# 7. Force upgrade when chart version is same
FORCE_UPGRADE="true" test_upgrade_logic "Force upgrade (same chart version)" "kube-ovn-1.17.0\ndocker.io/kubeovn/kube-ovn:v1.15.1" "25.03" 0

# 8. Specify OVN_VERSION_COMPATIBILITY manually
OVN_VERSION_COMPATIBILITY="99.99" test_upgrade_logic "Manual OVN_VERSION_COMPATIBILITY" "kube-ovn-1.17.0\ndocker.io/kubeovn/kube-ovn:v1.15.1" "99.99" 0

if [ $failed -eq 0 ]; then
  echo "ALL TESTS PASSED"
  exit 0
else
  echo "$failed TESTS FAILED"
  exit 1
fi
