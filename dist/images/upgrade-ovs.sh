#!/bin/bash

set -ex

OVN_DB_IPS=${OVN_DB_IPS:-}
ENABLE_SSL=${ENABLE_SSL:-false}
POD_NAMESPACE=${POD_NAMESPACE:-kube-system}
OVN_VERSION_COMPATIBILITY=${OVN_VERSION_COMPATIBILITY:-}

UPDATE_STRATEGY=`kubectl -n kube-system get ds ovs-ovn -o jsonpath='{.spec.updateStrategy.type}'`

SSL_OPTIONS=
function ssl_options() {
    if "$ENABLE_SSL" != "false" ]; then
        SSL_OPTIONS="-p /var/run/tls/key -c /var/run/tls/cert -C /var/run/tls/cacert"
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

nb_addr="$(gen_conn_str 6641)"
while true; do
  if [ x`ovn-nbctl --db=$nb_addr $SSL_OPTIONS get NB_Global . options | grep -o 'version_compatibility='` != "x" ]; then
    value=`ovn-nbctl --db=$nb_addr $SSL_OPTIONS get NB_Global . options:version_compatibility | sed -e 's/^"//' -e 's/"$//'`
    echo "ovn NB_Global option version_compatibility is set to $value"
    if [ "$value" = "$OVN_VERSION_COMPATIBILITY" -o "$value" = "_$OVN_VERSION_COMPATIBILITY" ]; then
      break
    fi
  fi
  echo "waiting for ovn NB_Global option version_compatibility to be set..."
  sleep 3
done

kubectl -n $POD_NAMESPACE rollout status deploy ovn-central --timeout=120s

if [ $UPDATE_STRATEGY = OnDelete ]; then
  dsChartVer=`kubectl get ds -n $POD_NAMESPACE ovs-ovn -o jsonpath={.spec.template.metadata.annotations.chart-version}`

  for node in `kubectl get node -o jsonpath='{.items[*].metadata.name}'`; do
    pods=(`kubectl -n $POD_NAMESPACE get pod -l app=ovs --field-selector spec.nodeName=$node -o name`)
    for pod in ${pods[*]}; do
      podChartVer=`kubectl -n $POD_NAMESPACE get $pod -o jsonpath={.metadata.annotations.chart-version}`
      if [ "$dsChartVer" != "$podChartVer" ]; then
        echo "deleting $pod on node $node"
        kubectl -n $POD_NAMESPACE delete $pod
      fi
    done

    while true; do
      pods=(`kubectl -n $POD_NAMESPACE get pod -l app=ovs --field-selector spec.nodeName=$node -o name`)
      if [ ${#pods[*]} -ne 0 ]; then
        break
      fi
      echo "waiting for ovs-ovn pod on node $node to be created"
      sleep 1
    done

    echo "waiting for ovs-ovn pod on node $node to be ready"
    kubectl -n $POD_NAMESPACE wait pod --for=condition=ready -l app=ovs --field-selector spec.nodeName=$node
  done
else
  kubectl -n $POD_NAMESPACE rollout status ds/ovs-ovn
fi

ovn-nbctl --db=$nb_addr $SSL_OPTIONS set NB_Global . options:version_compatibility=_$OVN_VERSION_COMPATIBILITY
