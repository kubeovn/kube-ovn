#!/bin/bash

set -ex

OVN_DB_IPS=${OVN_DB_IPS:-}
ENABLE_SSL=${ENABLE_SSL:-false}
POD_NAMESPACE=${POD_NAMESPACE:-kube-system}
OVN_VERSION_COMPATIBILITY=${OVN_VERSION_COMPATIBILITY:-}
CHART_NAME=${CHART_NAME:-kube-ovn}
CHART_VERSION=${CHART_VERSION:-}
FORCE_UPGRADE=${FORCE_UPGRADE:-false}

function semver_compare {
  local version1=$(echo "$1" | sed 's/^v//' | cut -d'-' -f1)
  local version2=$(echo "$2" | sed 's/^v//' | cut -d'-' -f1)

  if [[ "$version1" == "$version2" ]]; then
    echo 0
    return
  fi

  local i v1 v2
  IFS=. read -r -a v1 <<< "$version1"
  IFS=. read -r -a v2 <<< "$version2"

  for ((i=${#v1[@]}; i<${#v2[@]}; i++)); do
    v1[i]=0
  done
  for ((i=${#v2[@]}; i<${#v1[@]}; i++)); do
    v2[i]=0
  done

  for ((i=0; i<${#v1[@]}; i++)); do
    if (( 10#${v1[i]} > 10#${v2[i]} )); then
      echo 1
      return
    fi
    if (( 10#${v1[i]} < 10#${v2[i]} )); then
      echo -1
      return
    fi
  done
  echo 0
}

function detect_ovn_compatibility {
  # Ported from _helpers.tpl > kubeovn.ovn.versionCompatibility
  if [ -z "$OVN_VERSION_COMPATIBILITY" ]; then
    ds_info=$(kubectl -n "$POD_NAMESPACE" get ds ovs-ovn -o jsonpath='{.metadata.labels.helm\.sh/chart}{"\n"}{.spec.template.spec.containers[0].image}' 2>/dev/null || true)
    if [ -z "$ds_info" ]; then
      echo "DaemonSet ovs-ovn not found, skipping upgrade logic"
      return 0
    fi

    current_chart_version=$(echo "$ds_info" | head -n 1)
    image=$(echo "$ds_info" | tail -n 1)
    image_version=$(echo "$image" | sed 's/.*://' | sed 's/^v//')
    new_chart_version=$(echo "$CHART_NAME-$CHART_VERSION" | sed 's/+/_/g' | cut -c 1-63 | sed 's/-$//')

    if [ "$FORCE_UPGRADE" != "true" ] && [ "$new_chart_version" = "$current_chart_version" ]; then
      echo "Chart version hasn't changed ($new_chart_version), skipping upgrade logic"
      return 0
    fi

    # Regex check for image version (major.minor.patch)
    if [[ ! "$image_version" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
      echo "Image version $image_version is not a valid semver, skipping upgrade logic"
      return 0
    fi

    if [ "$(semver_compare "$image_version" "1.15.0")" -ge 0 ]; then
      OVN_VERSION_COMPATIBILITY="25.03"
    elif [ "$(semver_compare "$image_version" "1.13.0")" -ge 0 ]; then
      OVN_VERSION_COMPATIBILITY="24.03"
    elif [ "$(semver_compare "$image_version" "1.12.0")" -ge 0 ]; then
      OVN_VERSION_COMPATIBILITY="22.12"
    elif [ "$(semver_compare "$image_version" "1.11.0")" -ge 0 ]; then
      OVN_VERSION_COMPATIBILITY="22.03"
    else
      OVN_VERSION_COMPATIBILITY="21.06"
    fi
    echo "Detected OVN_VERSION_COMPATIBILITY=$OVN_VERSION_COMPATIBILITY for image version $image_version"
  fi
}

function gen_conn_str {
  if [[ -z "${OVN_DB_IPS}" ]]; then
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x="tcp:[${OVN_NB_SERVICE_HOST}]:${OVN_NB_SERVICE_PORT}"
    else
      x="ssl:[${OVN_NB_SERVICE_HOST}]:${OVN_NB_SERVICE_PORT}"
    fi
  else
    t=$(echo -n "${OVN_DB_IPS}" | sed 's/[[:space:]]//g' | sed 's/,/ /g')
    if [[ "$ENABLE_SSL" == "false" ]]; then
      x=$(for i in ${t}; do echo -n "tcp:[$i]:$1,"; done | sed 's/,$//')
    else
      x=$(for i in ${t}; do echo -n "ssl:[$i]:$1,"; done | sed 's/,$//')
    fi
  fi
  echo "$x"
}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  detect_ovn_compatibility

  if [ -z "$OVN_VERSION_COMPATIBILITY" ]; then
    echo "OVN_VERSION_COMPATIBILITY is not set and could not be detected, skipping upgrade logic"
    exit 0
  fi

  UPDATE_STRATEGY=$(kubectl -n "$POD_NAMESPACE" get ds ovs-ovn -o jsonpath='{.spec.updateStrategy.type}')

  SSL_OPTIONS=
  if [ "$ENABLE_SSL" != "false" ]; then
      SSL_OPTIONS="-p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert"
  fi

  nb_addr="$(gen_conn_str 6641)"
  while true; do
    if [ "x$(ovn-nbctl --db="$nb_addr" $SSL_OPTIONS get NB_Global . options | grep -o 'version_compatibility=')" != "x" ]; then
      value=$(ovn-nbctl --db="$nb_addr" $SSL_OPTIONS get NB_Global . options:version_compatibility | sed -e 's/^"//' -e 's/"$//')
      echo "ovn NB_Global option version_compatibility is already set to $value"
      if [ "$value" = "$OVN_VERSION_COMPATIBILITY" ] || [ "$value" = "_$OVN_VERSION_COMPATIBILITY" ]; then
        break
      fi
    fi
    echo "waiting for ovn NB_Global option version_compatibility to be set..."
    sleep 3
  done

  kubectl -n "$POD_NAMESPACE" rollout status deploy ovn-central --timeout=120s

  if [ "$UPDATE_STRATEGY" = "OnDelete" ]; then
    dsChartVer=$(kubectl get ds -n "$POD_NAMESPACE" ovs-ovn -o jsonpath='{.spec.template.metadata.labels.helm\.sh/chart}')

    for node in $(kubectl get node -o jsonpath='{.items[*].metadata.name}'); do
      pods=($(kubectl -n "$POD_NAMESPACE" get pod -l app=ovs --field-selector spec.nodeName="$node" -o name))
      for pod in "${pods[@]}"; do
        podChartVer=$(kubectl -n "$POD_NAMESPACE" get "$pod" -o jsonpath='{.metadata.labels.helm\.sh/chart}')
        if [ "$dsChartVer" != "$podChartVer" ]; then
          echo "deleting $pod on node $node"
          kubectl -n "$POD_NAMESPACE" delete "$pod"
        fi
      done

      while true; do
        pods=($(kubectl -n "$POD_NAMESPACE" get pod -l app=ovs --field-selector spec.nodeName="$node" -o name))
        if [ ${#pods[@]} -ne 0 ]; then
          break
        fi
        echo "waiting for ovs-ovn pod on node $node to be created"
        sleep 1
      done

      echo "waiting for ovs-ovn pod on node $node to be ready"
      kubectl -n "$POD_NAMESPACE" wait pod --for=condition=ready -l app=ovs --field-selector spec.nodeName="$node"
    done
  else
    kubectl -n "$POD_NAMESPACE" rollout status ds/ovs-ovn
  fi

  ovn-nbctl --db="$nb_addr" $SSL_OPTIONS set NB_Global . options:version_compatibility="_$OVN_VERSION_COMPATIBILITY"
fi
