#!/bin/bash
set -euo pipefail

OVN_NB_POD=

showHelp(){
  echo "sh ipsec.sh [init|start|stop|status]"
}

getOvnCentralPod(){
    NB_POD=$(kubectl get pod -n kube-system -l ovn-nb-leader=true | grep ovn-central | head -n 1 | awk '{print $1}')
    if [ -z "$NB_POD" ]; then
      echo "nb leader not exists"
      exit 1
    fi
    OVN_NB_POD=$NB_POD
}

initIpsec (){
  podNames=`kubectl get pod -n kube-system -l app=ovs -o 'jsonpath={.items[*].metadata.name}'`
  for pod in $podNames; do
    caPod=$pod
    break
  done

  echo " Initing CA $caPod "
  kubectl exec -it $caPod -n kube-system -- ovs-pki init --force > /dev/null

  for pod in $podNames; do
    echo " Initing privkey,req,cert file on pod $pod "
    systemId=$(kubectl exec -it ${pod} -n kube-system -- ovs-vsctl get Open_vSwitch . external_ids:system-id | tr -d '"' | tr -d '\r')

    kubectl exec -it $pod -n kube-system -- ovs-pki req -u $systemId --force > /dev/null
    kubectl exec -it $pod -n kube-system -- mv "${systemId}-privkey.pem" /etc/ipsec.d/private/
    kubectl exec -it $pod -n kube-system -- mv "${systemId}-req.pem" /etc/ipsec.d/reqs/

    if [[ $pod == $caPod ]]; then
       kubectl exec -it $pod -n kube-system -- rm -f "/etc/ipsec.d/reqs/${systemId}-cert.pem" > /dev/null
       kubectl exec -it $pod -n kube-system -- ovs-pki sign -b "/etc/ipsec.d/reqs/${systemId}" switch > /dev/null
       kubectl exec -it $pod -n kube-system -- mv "/etc/ipsec.d/reqs/${systemId}-cert.pem" /etc/ipsec.d/certs/
       kubectl exec -it $pod -n kube-system -- cp /var/lib/openvswitch/pki/switchca/cacert.pem /etc/ipsec.d/cacerts/ > /dev/null
    else
       kubectl cp "${pod}:/etc/ipsec.d/reqs/${systemId}-req.pem" "${systemId}-req.pem" -n kube-system > /dev/null
       kubectl cp "${systemId}-req.pem" "${caPod}:/kube-ovn/" -n kube-system > /dev/null
       # ovs-pki sign do not have options --force so rm cert first
       kubectl exec -it $caPod -n kube-system -- rm -f "/kube-ovn/${systemId}-cert.pem"
       kubectl exec -it $caPod -n kube-system -- ovs-pki sign -b ${systemId} switch > /dev/null
       kubectl cp "${caPod}:/kube-ovn/${systemId}-cert.pem" "${systemId}-cert.pem" -n kube-system > /dev/null
       kubectl cp "${systemId}-cert.pem" "${pod}:/etc/ipsec.d/certs/" -n kube-system > /dev/null

       kubectl cp "${caPod}:/var/lib/openvswitch/pki/switchca/cacert.pem" cacert.pem -n kube-system > /dev/null
       kubectl cp cacert.pem "${pod}:/etc/ipsec.d/cacerts/" -n kube-system > /dev/null

       # clean temp files
       kubectl exec -it $caPod -n kube-system -- rm -f "/kube-ovn/${systemId}-req.pem"
       kubectl exec -it $caPod -n kube-system -- rm -f "/kube-ovn/${systemId}-cert.pem"
       rm -f ${systemId}-req.pem
       rm -f ${systemId}-cert.pem
       rm -f cacert.pem
    fi

    kubectl exec -it $pod -n kube-system -- ovs-vsctl set Open_vSwitch . \
          other_config:certificate=/etc/ipsec.d/certs/"${systemId}-cert.pem" \
          other_config:private_key=/etc/ipsec.d/private/"${systemId}-privkey.pem" \
          other_config:ca_cert=/etc/ipsec.d/cacerts/cacert.pem
  done

  echo " Enabling ovn ipsec "
  kubectl ko nbctl set nb_global . ipsec=true

  for pod in $podNames; do
      echo " Starting pod ${pod} ipsec service"
      kubectl exec -it -n kube-system $pod -- service openvswitch-ipsec restart > /dev/null
      kubectl exec -it -n kube-system $pod -- service ipsec restart > /dev/null
  done

  echo " Kube-OVN ipsec init successfully, it may take a few seconds to setup ipsec completely "
}

getOvnCentralPod
subcommand="$1"; shift

case $subcommand in
  init)
    initIpsec
    ;;
  start)
    kubectl exec "$OVN_NB_POD" -n kube-system -c ovn-central -- ovn-nbctl set nb_global . ipsec=true
    echo " Kube-OVN ipsec started "
    ;;
  stop)
    kubectl exec "$OVN_NB_POD" -n kube-system -c ovn-central -- ovn-nbctl set nb_global . ipsec=false
    echo " Kube-OVN ipsec stopped "
    ;;
  status)
    podNames=`kubectl get pod -n kube-system -l app=ovs -o 'jsonpath={.items[*].metadata.name}'`
    for pod in $podNames; do
      echo " Pod {$pod} ipsec status..."
      kubectl exec -it $pod -n kube-system -- ovs-appctl -t ovs-monitor-ipsec tunnels/show
    done
    ;;
  *)
    showHelp
    exit 1
    ;;
esac