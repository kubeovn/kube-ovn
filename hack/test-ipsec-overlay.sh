#!/usr/bin/env bash
# Variables in remote bash snippets must be expanded inside the container.
# shellcheck disable=SC2016
set -euo pipefail

# Run against a disposable, two-node Kind cluster on CI's Docker runner.
# Capture the actual underlay interface, not the decrypted pod interface.
overlay_image=${1:?candidate image is required}
overlay_protection=${IPSEC_PROTECTION_FAILURES:-false}
overlay_cleanup=${IPSEC_CLEANUP_STARTUP:-false}
overlay_installer=${IPSEC_OVERLAY_INSTALLER:-false}
overlay_family=${IPSEC_OVERLAY_FAMILY:-IPv4}
overlay_tunnel=${IPSEC_OVERLAY_TUNNEL:-geneve}
case "$overlay_family" in
  IPv4) overlay_kind_family=ipv4; overlay_pods=10.16.0.0/16; overlay_services=10.96.0.0/12; overlay_wildcard=0.0.0.0/0; overlay_ping=ping ;;
  IPv6) overlay_kind_family=ipv6; overlay_pods=fd00:10:16::/56; overlay_services=fd00:10:96::/112; overlay_wildcard=::/0; overlay_ping=ping6 ;;
  *) echo 'Unsupported overlay IP family' >&2; exit 1 ;;
esac
case "$overlay_tunnel" in
  geneve|vxlan) ;;
  *) echo 'Unsupported overlay tunnel type' >&2; exit 1 ;;
esac
overlay_cluster="ipsec-overlay-${GITHUB_RUN_ID:?requires an isolated CI runner}-${GITHUB_RUN_ATTEMPT:-1}"
overlay_kubeconfig=$(mktemp)
overlay_config=$(mktemp)
chmod 600 "$overlay_kubeconfig"
export KUBECONFIG="$overlay_kubeconfig"
overlay_created=false
installer_dir=
diagnose() {
  kubectl get pods -A -o wide || true
  kubectl get events -A --sort-by=.lastTimestamp | tail -60 || true
  # Coordination objects and these fields contain only public state. Do not
  # print Secrets, certificate requests or complete XFRM state/SA keys.
  kubectl -n kube-system get configmap ovn-ipsec-coordination -o jsonpath='{.data.state}' || true
  kubectl get nodes -o json | python3 -c '
import base64,json,sys
for node in json.load(sys.stdin)["items"]:
    raw=node["metadata"].get("annotations",{}).get("kube-ovn.io/ipsec-receipt")
    if not raw:
        print(node["metadata"]["name"], "no IPsec receipt")
        continue
    try:
        claim=json.loads(base64.b64decode(json.loads(raw)["payload"]))
        status=claim["status"]
        print(node["metadata"]["name"], claim["phase"], claim["observed"],
              {key:status.get(key) for key in ("phase","reason","runtimeHealthy","configurationApplied","protectionArmed")})
    except (ValueError,KeyError,TypeError):
        print(node["metadata"]["name"], "invalid IPsec receipt envelope")
' || true
  while read -r central; do
    [[ -n "$central" ]] || continue
    kubectl -n kube-system exec "$central" -- ovn-nbctl get NB_Global . ipsec || true
    kubectl -n kube-system exec "$central" -- ovn-sbctl get SB_Global . ipsec || true
  done < <(kubectl -n kube-system get pods -l app=ovn-central -o name)
  for component in ovs kube-ovn-controller kube-ovn-cni; do
    while read -r pod; do
      [[ -n "$pod" ]] || continue
      if [[ "$component" == kube-ovn-cni ]]; then
        kubectl -n kube-system logs "$pod" -c cni-server --tail=80 || true
        kubectl -n kube-system logs "$pod" -c ipsec --tail=80 || true
        kubectl -n kube-system logs "$pod" -c ipsec-cleanup --tail=80 || true
      else
        kubectl -n kube-system logs "$pod" --tail=80 || true
      fi
    done < <(kubectl -n kube-system get pods -l "app=$component" -o name)
  done
}
cleanup() {
  overlay_status=$?
  if [[ "$overlay_created" == true ]]; then
    if [[ "$overlay_status" != 0 ]]; then
      diagnose
    fi
    kind delete cluster --name "$overlay_cluster"
  fi
  rm -f "$overlay_kubeconfig" "$overlay_config"
  if [[ -n "$installer_dir" ]]; then
    rm -rf "$installer_dir"
  fi
  exit "$overlay_status"
}
trap cleanup EXIT
cat >"$overlay_config" <<EOF
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
networking:
  disableDefaultCNI: true
  ipFamily: $overlay_kind_family
  podSubnet: $overlay_pods
  serviceSubnet: $overlay_services
nodes:
  - role: control-plane
  - role: worker
EOF
kind create cluster --name "$overlay_cluster" --config "$overlay_config" \
  --image kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5 \
  --kubeconfig "$overlay_kubeconfig" --wait 120s
overlay_created=true
kind load docker-image --name "$overlay_cluster" "$overlay_image"
overlay_control_plane="$overlay_cluster-control-plane"
overlay_worker="$overlay_cluster-worker"
kubectl label node "$overlay_control_plane" kube-ovn/role=master
kubectl taint node "$overlay_control_plane" node-role.kubernetes.io/control-plane:NoSchedule-
if [[ "$overlay_installer" == true ]]; then
  # Exercise the same phased installer used by the upstream IPsec E2E job.
  # Keep generated manifests inside a temporary directory on this CI runner.
  installer_dir=$(mktemp -d)
  docker tag "$overlay_image" "docker.io/kubeovn/kube-ovn:${overlay_image##*:}"
  kind load docker-image --name "$overlay_cluster" "docker.io/kubeovn/kube-ovn:${overlay_image##*:}"
  sed "s/^VERSION=.*/VERSION=\"${overlay_image##*:}\"/" dist/images/install.sh > "$installer_dir/install.sh"
  (
    cd "$installer_dir"
    ENABLE_OVN_IPSEC=true ENABLE_IC=false DEL_NON_HOST_NET_POD=false \
      ENABLE_SSL=false IPV6=false DUAL_STACK=false bash install.sh
  )
  rm -rf "$installer_dir"
else
  helm install kube-ovn charts/kube-ovn-v2 --namespace kube-system \
  --set-string global.registry.address= \
  --set-string "global.images.kubeovn.repository=${overlay_image%:*}" \
  --set-string "global.images.kubeovn.tag=${overlay_image##*:}" \
  --set image.pullPolicy=Never --set features.enableOvnIpsec=true \
  --set ovsOvn.disableModulesManagement=true \
  --set-string "networking.stack=$overlay_family" --set-string "networking.tunnelType=$overlay_tunnel"
fi
kubectl -n kube-system rollout status deployment/ovn-central --timeout=240s
kubectl -n kube-system rollout status deployment/kube-ovn-controller --timeout=240s
kubectl -n kube-system rollout status daemonset/ovs-ovn --timeout=240s
kubectl -n kube-system rollout status daemonset/kube-ovn-cni --timeout=300s
# Confirm the requested privilege/priority split in the deployed processes,
# beyond rendered manifests and the isolated runtime fixture.
for node in "$overlay_control_plane" "$overlay_worker"; do
  cni_pod=$(kubectl -n kube-system get pod -l app=kube-ovn-cni --field-selector "spec.nodeName=$node" -o name)
  kubectl -n kube-system exec "$cni_pod" -c cni-server -- python3 -c '
import os,subprocess
pids=subprocess.check_output(["pidof","kube-ovn-daemon"],text=True).split()
assert len(pids)==1, "expected one node CNI process"
pid=int(pids[0])
with open(f"/proc/{pid}/status") as f:
    status=dict(line.split(":",1) for line in f if ":" in line)
assert status["Uid"].split()[1]=="65534", "CNI must remain non-root"
assert int(status["CapBnd"],16)&(1<<23)==0, "CNI must not retain SYS_NICE in its bounding set"
assert int(status["CapEff"],16)&(1<<23)==0, "CNI must not have effective SYS_NICE"
assert os.getpriority(os.PRIO_PROCESS,pid)==0, "CNI must use normal process priority"
print("Deployed CNI: UID 65534, nice 0, SYS_NICE absent")'
  kubectl -n kube-system exec "$cni_pod" -c ipsec -- python3 -c '
import os,subprocess
pids=subprocess.check_output(["pidof","charon"],text=True).split()
assert len(pids)==1, "expected one owned IKE process"
pid=int(pids[0])
with open(f"/proc/{pid}/status") as f:
    status=dict(line.split(":",1) for line in f if ":" in line)
assert status["Uid"].split()[1]=="0", "IKE must use the validated root runtime"
assert int(status["CapEff"],16)&~((1<<12)|(1<<10)|(1<<23))==0, "IKE must stay within the three production capabilities"
assert os.getpriority(os.PRIO_PROCESS,pid)==-5, "IPsec must own the configured process priority"
print("Deployed IPsec: IKE nice -5, effective capabilities restricted")'
done
declare -A overlay_mark overlay_reqid overlay_guard
  # Read public production leases only; no fixture may overwrite the owner.
  for node in "$overlay_control_plane" "$overlay_worker"; do
    cni_pod=$(kubectl -n kube-system get pod -l app=kube-ovn-cni --field-selector "spec.nodeName=$node" -o name)
    lease_public=$(kubectl -n kube-system exec "$cni_pod" -c ipsec -- python3 -c '
import json,sys
with open("/etc/ovs_ipsec_keys/protection.json") as f:
    lease=json.load(f)
assert lease["required"] and lease["mark"] > 0 and lease["reqid"] > 0
print(lease["nodeUID"],lease["mark"],lease["reqid"],lease["indexes"][int(sys.argv[1]=="IPv6")])' "$overlay_family")
    read -r lease_uid lease_mark lease_reqid lease_guard <<<"$lease_public"
    [[ "$lease_uid" == "$(kubectl get node "$node" -o jsonpath='{.metadata.uid}')" ]]
    [[ "$lease_mark" =~ ^[0-9]+$ && "$lease_reqid" =~ ^[0-9]+$ && "$lease_guard" =~ ^[0-9]+$ ]]
    overlay_mark[$node]=$lease_mark
    overlay_reqid[$node]=$lease_reqid
    overlay_guard[$node]=$lease_guard
    ovs_pod=$(kubectl -n kube-system get pod -l app=ovs --field-selector "spec.nodeName=$node" -o name)
    for attempt in {1..60}; do
      if kubectl -n kube-system exec "$ovs_pod" -- ovs-vsctl --format=json --columns=options find Interface "type=$overlay_tunnel" | \
          python3 -c 'import json,sys; rows=json.load(sys.stdin)["data"]; expected={"egress_pkt_mark":sys.argv[1],"ipsec_mark_out":sys.argv[1]+"/0xffffffff","ipsec_reqid":sys.argv[2]}; sys.exit(not rows or not all(expected.items() <= dict(row[0][1]).items() for row in rows))' "$lease_mark" "$lease_reqid"; then
        break
      fi
      if [[ "$attempt" == 60 ]]; then
        echo 'OVN did not generate the owned output mark and IKE selectors' >&2
        exit 1
      fi
      sleep 1
    done
  done
kubectl create namespace ipsec-overlay
for role in control-plane worker; do
  kubectl -n ipsec-overlay run "$role" --image="$overlay_image" --image-pull-policy=Never \
    --overrides="{\"spec\":{\"nodeName\":\"$overlay_cluster-$role\"}}" --command -- sleep 600
done
kubectl -n ipsec-overlay wait pod --all --for=condition=Ready --timeout=180s
overlay_peer=$(kubectl get node "$overlay_worker" -o json | python3 -c '
import ipaddress,json,sys
family=4 if sys.argv[1]=="IPv4" else 6
addresses=[a["address"] for a in json.load(sys.stdin)["status"]["addresses"] if a["type"]=="InternalIP" and ipaddress.ip_address(a["address"]).version==family]
assert len(addresses)==1, "expected one peer InternalIP for the requested address family"
print(addresses[0])' "$overlay_family")
overlay_ovs=$(kubectl -n kube-system get pod -l app=ovs --field-selector "spec.nodeName=$overlay_control_plane" -o name)
kubectl -n kube-system exec "$overlay_ovs" -- bash -c '
  set -euo pipefail
  tcpdump -Z root -i eth0 -p -n -U -w /tmp/ipsec-overlay.pcap \
    "host $1 and (udp port 6081 or udp port 4789 or udp port 4500 or ip proto 50 or ip6 proto 50)" >/tmp/ipsec-overlay-capture.log 2>&1 &
  overlay_capture_pid=$!
  echo "$overlay_capture_pid" >/tmp/ipsec-overlay-capture.pid
  tcpdump -Z root -i eth0 -Q out -p -n -U -w /tmp/ipsec-overlay-egress.pcap \
    "host $1 and (udp port 6081 or udp port 4789 or udp port 4500 or ip proto 50 or ip6 proto 50)" >/tmp/ipsec-overlay-egress.log 2>&1 &
  overlay_egress_pid=$!
  echo "$overlay_egress_pid" >/tmp/ipsec-overlay-egress.pid
  wait "$overlay_capture_pid"
  wait "$overlay_egress_pid"
  touch /tmp/ipsec-overlay-capture-complete
' overlay-capture "$overlay_peer" &
overlay_capture_client=$!
for attempt in {1..30}; do
  if kubectl -n kube-system exec "$overlay_ovs" -- bash -c 'grep -q "listening on eth0" /tmp/ipsec-overlay-capture.log && grep -q "listening on eth0" /tmp/ipsec-overlay-egress.log && kill -0 "$(cat /tmp/ipsec-overlay-capture.pid)" && kill -0 "$(cat /tmp/ipsec-overlay-egress.pid)"'; then
    break
  fi
  if [[ "$attempt" == 30 ]]; then
    echo 'Underlay packet capture did not start' >&2
    exit 1
  fi
  sleep 0.1
done
for direction in control-plane worker; do
  destination=control-plane
  if [[ "$direction" == control-plane ]]; then
    destination=worker
  fi
  overlay_pod_ip=$(kubectl -n ipsec-overlay get pod "$destination" -o jsonpath='{.status.podIP}')
  # The image's inetutils ping6 does not implement iputils' -W option.
  # Bound the whole invocation with the same coreutils timeout for both tools.
  kubectl -n ipsec-overlay exec "$direction" -- timeout 45 "$overlay_ping" -c 20 "$overlay_pod_ip"
done
kubectl -n kube-system exec "$overlay_ovs" -- bash -c '
  kill -INT "$(cat /tmp/ipsec-overlay-capture.pid)"
  kill -INT "$(cat /tmp/ipsec-overlay-egress.pid)"
  for attempt in {1..30}; do
    if test -f /tmp/ipsec-overlay-capture-complete; then
      exit 0
    fi
    sleep 0.1
  done
  exit 1
'
wait "$overlay_capture_client"
overlay_esp=$(kubectl -n kube-system exec "$overlay_ovs" -- bash -o pipefail -c 'tcpdump -Z root -n -r /tmp/ipsec-overlay.pcap | awk "/ESP\\(spi=/{count++} END{print count+0}"')
overlay_plaintext=$(kubectl -n kube-system exec "$overlay_ovs" -- bash -o pipefail -c 'tcpdump -Z root -n -r /tmp/ipsec-overlay.pcap "udp port 6081 or udp port 4789" | wc -l')
echo "Actual cross-node OVN $overlay_family $overlay_tunnel pods: ESP packets=$overlay_esp plaintext transport packets=$overlay_plaintext"
if [[ "$overlay_esp" == 0 || "$overlay_plaintext" != 0 ]]; then
  # Preserve the rejection and distinguish physical egress from ingress
  # observations before investigating a possible unencrypted packet.
  kubectl -n kube-system exec "$overlay_ovs" -- tcpdump -Z root -n -vv -r /tmp/ipsec-overlay.pcap 'udp port 6081 or udp port 4789'
  kubectl -n kube-system exec "$overlay_ovs" -- tcpdump -Z root -n -vv -r /tmp/ipsec-overlay-egress.pcap 'udp port 6081 or udp port 4789'
  echo 'The actual OVN overlay did not prove encryption without plaintext' >&2
  exit 1
fi

if [[ "$overlay_protection" == true ]]; then
  for node in "$overlay_control_plane" "$overlay_worker"; do
    # Do not dump XFRM key material; print only the counted owned reqid headers.
    owned_states=$(docker exec "$node" bash -o pipefail -c 'ip xfrm state | awk "/reqid $1 /{n++} END{print n+0}"' owned-states "${overlay_reqid[$node]}")
    if [[ "$owned_states" == 0 ]]; then
      echo 'No ESP SA uses the production protection reservation' >&2
      exit 1
    fi
    echo "OVN-generated marked $overlay_family $overlay_tunnel tunnel on $node: owned ESP states=$owned_states"
  done
fi

if [[ "$overlay_protection" == true ]]; then
  # Freeze kubelet recovery only in disposable Kind nodes. CRI exec continues
  # to exercise existing containers without depending on the stopped kubelet.
  for node in "$overlay_control_plane" "$overlay_worker"; do
    docker exec "$node" systemctl stop kubelet
    ipsec_id=$(docker exec "$node" crictl ps --name '^ipsec$' -q)
    [[ -n "$ipsec_id" && "$ipsec_id" != *$'\n'* ]]
    docker exec "$node" crictl stop --timeout 10 "$ipsec_id"
    for attempt in {1..30}; do
      owned_states=$(docker exec "$node" bash -o pipefail -c 'ip xfrm state | awk "/reqid $1 /{n++} END{print n+0}"' owned-states "${overlay_reqid[$node]}")
      if [[ "$owned_states" == 0 ]]; then
        break
      fi
      if [[ "$attempt" == 30 ]]; then
        echo 'Stopped IKE runtime retained production ESP SAs' >&2
        exit 1
      fi
      sleep 1
    done
    docker exec "$node" bash -o pipefail -c 'ip xfrm policy get index "$1" dir out mark "$2" mask 0xffffffff | grep -q "action block"' guard-check "${overlay_guard[$node]}" "${overlay_mark[$node]}"
  done
  central_id=$(docker exec "$overlay_control_plane" crictl ps --name '^ovn-central$' -q)
  [[ -n "$central_id" && "$central_id" != *$'\n'* ]]
  docker exec "$overlay_control_plane" crictl exec "$central_id" ovn-nbctl set NB_Global . ipsec=false
  ovs_id=$(docker exec "$overlay_control_plane" crictl ps --name '^openvswitch$' -q)
  [[ -n "$ovs_id" && "$ovs_id" != *$'\n'* ]]
  for attempt in {1..30}; do
    if docker exec "$overlay_control_plane" crictl exec "$ovs_id" ovs-vsctl --format=json --columns=options find Interface "type=$overlay_tunnel" | \
        python3 -c 'import json,sys; rows=json.load(sys.stdin)["data"]; options=[dict(row[0][1]) for row in rows]; sys.exit(not options or not all(o.get("egress_pkt_mark")==sys.argv[1] and "remote_name" not in o for o in options))' "${overlay_mark[$overlay_control_plane]}"; then
      break
    fi
    if [[ "$attempt" == 30 ]]; then
      echo 'OVN did not retain the protection mark with SB encryption disabled' >&2
      exit 1
    fi
    sleep 1
  done
  docker exec "$overlay_control_plane" crictl exec "$ovs_id" bash -c '
    set -euo pipefail
    tcpdump -Z root -i eth0 -p -n -U -w /tmp/ipsec-protected.pcap \
      "host $1 and (udp port 6081 or udp port 4789)" >/tmp/ipsec-protected.log 2>&1 &
    echo "$!" >/tmp/ipsec-protected.pid
    wait "$!"
  ' protected-capture "$overlay_peer" &
  protected_capture_client=$!
  for attempt in {1..30}; do
    if docker exec "$overlay_control_plane" crictl exec "$ovs_id" bash -c 'grep -q "listening on eth0" /tmp/ipsec-protected.log'; then
      break
    fi
    if [[ "$attempt" == 30 ]]; then
      echo 'Protected-output capture did not start' >&2
      exit 1
    fi
    sleep 0.1
  done
  pod_id=$(docker exec "$overlay_control_plane" crictl ps --name '^control-plane$' -q)
  [[ -n "$pod_id" && "$pod_id" != *$'\n'* ]]
  overlay_pod_ip=$(kubectl -n ipsec-overlay get pod worker -o jsonpath='{.status.podIP}')
  if docker exec "$overlay_control_plane" crictl exec "$pod_id" timeout 10 "$overlay_ping" -c 5 "$overlay_pod_ip"; then
    echo 'Protected cross-node traffic was delivered with IKE stopped and SB encryption disabled' >&2
    exit 1
  fi
  # These upstream paths have no per-peer IKE identity. They are unsupported
  # encryption modes and must retain the output guard, including existing ports.
  for node in "$overlay_control_plane" "$overlay_worker"; do
    node_ovs_id=$(docker exec "$node" crictl ps --name '^openvswitch$' -q)
    [[ -n "$node_ovs_id" && "$node_ovs_id" != *$'\n'* ]]
    docker exec "$node" crictl exec "$node_ovs_id" ovs-vsctl set Open_vSwitch . \
      external_ids:ovn-enable-flow-based-tunnels=true \
      external_ids:ovn-evpn-vxlan-ports=4789
    for expected_mark in "${overlay_mark[$node]}" 759816; do
      if [[ "$expected_mark" == 759816 ]]; then
        # Install the replacement fixture guard before publishing its mark.
        docker exec "$node" ip xfrm policy add src "$overlay_wildcard" dst "$overlay_wildcard" \
          dir out priority 2147483647 index 759841 action block mark 759816 mask 0xffffffff
        docker exec "$node" crictl exec "$node_ovs_id" ovs-vsctl set Open_vSwitch . \
          external_ids:ovn-ipsec-protection-mark=759816
      fi
      for attempt in {1..30}; do
        if docker exec "$node" crictl exec "$node_ovs_id" ovs-vsctl --format=json --columns=name,options list Interface | \
            python3 -c 'import json,sys; rows=json.load(sys.stdin)["data"]; ports=[(name,dict(options[1])) for name,options in rows if name.startswith("ovn") and dict(options[1]).get("remote_ip")=="flow"]; sys.exit(not any("-evpn-" in name for name,_ in ports) or not any("-evpn-" not in name for name,_ in ports) or not all(o.get("egress_pkt_mark")==sys.argv[1] and "remote_name" not in o and "ipsec_mark_out" not in o and "ipsec_reqid" not in o for _,o in ports))' "$expected_mark"; then
          break
        fi
        if [[ "$attempt" == 30 ]]; then
          echo "OVN did not reconcile flow-based/EVPN output mark $expected_mark on $node" >&2
          exit 1
        fi
        sleep 1
      done
    done
  done
  if docker exec "$overlay_control_plane" crictl exec "$pod_id" timeout 10 "$overlay_ping" -c 5 "$overlay_pod_ip"; then
    echo 'Unsupported flow-based output bypassed the armed protection' >&2
    exit 1
  fi
  docker exec "$overlay_control_plane" crictl exec "$ovs_id" bash -c 'kill -INT "$(cat /tmp/ipsec-protected.pid)"'
  wait "$protected_capture_client"
  protected_plaintext=$(docker exec "$overlay_control_plane" crictl exec "$ovs_id" bash -o pipefail -c 'tcpdump -Z root -n -r /tmp/ipsec-protected.pcap | wc -l')
  echo "OVN $overlay_family $overlay_tunnel retained-mark protection after IKE stop/SB disable: plaintext transport packets=$protected_plaintext"
  [[ "$protected_plaintext" == 0 ]]
fi

if [[ "$overlay_cleanup" == true ]]; then
  [[ "$overlay_protection" == false ]]
  declare -A cleanup_lease
  for node in "$overlay_control_plane" "$overlay_worker"; do
    cni_pod=$(kubectl -n kube-system get pod -l app=kube-ovn-cni --field-selector "spec.nodeName=$node" -o name)
    cleanup_lease[$node]=$(kubectl -n kube-system exec "$cni_pod" -c ipsec -- python3 -c '
import json
with open("/etc/ovs_ipsec_keys/protection.json") as f:
    lease=json.load(f)
print(lease["lease"],lease["mark"],lease["reqid"])')
  done
  # Exercise sequential cleanup init containers with maxUnavailable=1.
  # Each node must release locally and exit so the next Pod can roll out.
  helm upgrade kube-ovn charts/kube-ovn-v2 --namespace kube-system --reuse-values \
    --set features.enableOvnIpsec=false
  kubectl -n kube-system rollout status deployment/kube-ovn-controller --timeout=240s
  kubectl -n kube-system rollout status daemonset/kube-ovn-cni --timeout=300s
  kubectl -n kube-system rollout status daemonset/ovs-ovn --timeout=300s
  for node in "$overlay_control_plane" "$overlay_worker"; do
    cni_pod=$(kubectl -n kube-system get pod -l app=kube-ovn-cni --field-selector "spec.nodeName=$node" -o name)
    kubectl -n kube-system get "$cni_pod" -o json | python3 -c '
import json,sys
pod=json.load(sys.stdin)
assert all(c["name"]!="ipsec-cleanup" for c in pod["spec"]["containers"]), "cleanup is still a resident container"
init=next(c for c in pod["spec"]["initContainers"] if c["name"]=="ipsec-cleanup")
assert "restartPolicy" not in init, "cleanup must be an ordinary finite init"
assert "SYS_NICE" not in init["securityContext"]["capabilities"]["add"]
status=next(c for c in pod["status"]["initContainerStatuses"] if c["name"]=="ipsec-cleanup")
assert status["state"]["terminated"]["exitCode"]==0, "cleanup init did not finish successfully"
'
    docker exec "$node" cat /etc/ovs_ipsec_keys/protection.json | python3 -c '
import json,sys
reservation=json.load(sys.stdin)
assert not reservation.get("required", False) and not any(reservation["indexes"]), "cleanup retained active protection intent"
assert " ".join(str(reservation[key]) for key in ("lease","mark","reqid"))==sys.argv[1], "cleanup lost its inactive ownership checkpoint"
' "${cleanup_lease[$node]}"
    ovs_pod=$(kubectl -n kube-system get pod -l app=ovs --field-selector "spec.nodeName=$node" -o name)
    kubectl -n kube-system exec "$ovs_pod" -- bash -c '
      set -euo pipefail
      test ! -e /run/kube-ovn-ipsec-protection/required
      test -z "$(pidof charon || true)"
      for key in certificate private_key ca_cert; do
        test "$(ovs-vsctl --if-exists get Open_vSwitch . other_config:$key)" = "[]"
      done
      for key in ovn-ipsec-protection-node-uid ovn-ipsec-protection-lease ovn-ipsec-protection-mark ovn-ipsec-protection-reqid; do
        test "$(ovs-vsctl --if-exists get Open_vSwitch . external_ids:$key)" = "[]"
      done
      /usr/share/openvswitch/scripts/ovs-ctl status
      /usr/share/ovn/scripts/ovn-ctl status_controller
    '
    kubectl -n kube-system exec "$ovs_pod" -- ip xfrm policy | python3 -c '
import re,sys
mark=int(sys.argv[1].split()[1])
marks=re.findall(r"\bmark\s+(0x[0-9a-fA-F]+|[0-9]+)/", sys.stdin.read())
assert all(int(value,0)!=mark for value in marks), "cleanup retained an owned kernel guard"
print("Cleanup init exited: owned identity, output lease and guards released")
' "${cleanup_lease[$node]}"
  done
  for attempt in {1..60}; do
    if kubectl -n kube-system get configmap ovn-ipsec-coordination -o jsonpath='{.data.state}' | \
        python3 -c 'import json,sys; sys.exit(json.load(sys.stdin)["phase"]!="Disabled")'; then
      break
    fi
    if [[ "$attempt" == 60 ]]; then
      echo 'Controller did not acknowledge all release receipts' >&2
      exit 1
    fi
    sleep 2
  done
fi
