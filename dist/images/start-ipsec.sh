#!/bin/bash
set -euo pipefail

podNames=`kubectl get pod -n kube-system -l app=ovs -o 'jsonpath={.items[*].metadata.name}'`
for pod in $podNames; do
  caPod=$pod
  break
done

echo " initing CA $caPod "
kubectl exec -it $caPod -n kube-system -- ovs-pki init --force > /dev/null

for pod in $podNames; do
  echo " initing privkey,req,cert file on pod $pod "
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

     kubectl exec -it $caPod -n kube-system -- rm -f "/kube-ovn/${systemId}-req.pem"
     kubectl exec -it $caPod -n kube-system -- rm -f "/kube-ovn/${systemId}-cert.pem"
  fi

  kubectl exec -it $pod -n kube-system -- ovs-vsctl set Open_vSwitch . \
        other_config:certificate=/etc/ipsec.d/certs/"${systemId}-cert.pem" \
        other_config:private_key=/etc/ipsec.d/private/"${systemId}-privkey.pem" \
        other_config:ca_cert=/etc/ipsec.d/cacerts/cacert.pem
done

echo " enabling ovn ipsec "
kubectl ko nbctl set nb_global . ipsec=true

for pod in $podNames; do
    echo " starting pod ${pod} ipsec service"
    kubectl exec -it -n kube-system $pod -- service openvswitch-ipsec restart > /dev/null
    kubectl exec -it -n kube-system $pod -- service ipsec restart > /dev/null
done

echo " kube-ovn ipsec start successfully, it may take a few seconds to setup ipsec completely "

