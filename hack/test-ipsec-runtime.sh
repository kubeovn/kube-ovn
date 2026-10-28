#!/usr/bin/env bash
set -euo pipefail

# This harness uses private Docker network namespaces. It never attaches an
# IPsec daemon to the runner's host network or touches host IKE processes.
candidate_image=${1:?candidate image is required}
runtime_binary=${2:?compiled pkg/ipsec test binary is required}
runtime_binary=$(realpath "$runtime_binary")
runtime_id="ipsec-runtime-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}"
ovs_container="$runtime_id-ovs"
test_container="$runtime_id-test"
runtime_volume="$runtime_id-socket"

cleanup() {
  if docker cp "$ovs_container:/tmp/ovsdb-fixture.log" /tmp/ovsdb-fixture.log 2>/dev/null; then
    cat /tmp/ovsdb-fixture.log
    rm -f /tmp/ovsdb-fixture.log
  fi
  docker logs "$ovs_container" 2>/dev/null || true
  docker rm -f "$test_container" "$ovs_container" >/dev/null 2>&1 || true
  docker volume rm "$runtime_volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker image inspect "$candidate_image" --format '{{.Id}} {{json .Config.Labels}}'
docker volume create "$runtime_volume" >/dev/null
# Match hostpath-init ownership and exercise the production socket permissions.
# CHOWN belongs only to this disposable volume setup, never the IPsec agent.
docker run --rm --network none --user 0:0 --cap-drop ALL --cap-add CHOWN \
  --mount "type=volume,src=$runtime_volume,dst=/run/openvswitch,volume-nocopy" \
  "$candidate_image" bash -c '
    set -euo pipefail
    chown 65534:65534 /run/openvswitch
    stat -c "%u:%g %a" /run/openvswitch
  '
docker run --detach --name "$ovs_container" --network none --user 65534:65534 \
  --cap-drop ALL --cap-add NET_BIND_SERVICE \
  --memory 256m --cpus 1 \
  --mount "type=volume,src=$runtime_volume,dst=/run/openvswitch,volume-nocopy" \
  "$candidate_image" bash -c '
    exec >/tmp/ovsdb-fixture.log 2>&1
    set -exuo pipefail
    id
    stat -c "%u:%g %a" /run/openvswitch /tmp /usr/share/openvswitch/vswitch.ovsschema
    getcap /usr/bin/ovsdb-tool /usr/sbin/ovsdb-server
    ovsdb-tool create /tmp/ipsec-test.db /usr/share/openvswitch/vswitch.ovsschema
    umask 0007
    exec ovsdb-server /tmp/ipsec-test.db --remote=punix:/run/openvswitch/db.sock \
      --pidfile=/run/openvswitch/ovsdb-server.pid --unixctl=/run/openvswitch/db.ctl
  ' >/dev/null

for attempt in {1..30}; do
  if docker exec "$ovs_container" ovs-vsctl --timeout=1 --no-wait init; then
    break
  fi
  if [[ "$(docker inspect --format '{{.State.Running}}' "$ovs_container")" != true ]]; then
    docker inspect --format '{{json .State}}' "$ovs_container" >&2
    echo 'Test OVSDB exited before becoming ready' >&2
    exit 1
  fi
  if [[ "$attempt" == 30 ]]; then
    echo 'Test OVSDB did not start' >&2
    exit 1
  fi
  sleep 1
done
docker exec "$ovs_container" ovs-vsctl --timeout=5 --no-wait set Open_vSwitch . external_ids:system-id=runtime-test-chassis

docker run --rm --network none --user 0:65534 \
  --cap-drop ALL --cap-add NET_ADMIN --cap-add NET_BIND_SERVICE \
  --memory 256m --cpus 1 --security-opt no-new-privileges \
  --mount "type=volume,src=$runtime_volume,dst=/run/openvswitch,volume-nocopy,readonly" \
  --mount "type=bind,src=$runtime_binary,dst=/tmp/ipsec-runtime-tests,readonly" \
  --env KUBE_OVN_IPSEC_RUNTIME_TEST=true \
  "$candidate_image" /tmp/ipsec-runtime-tests -test.run '^TestCandidate(Cleanup|InitialPreparation|CancelledPreparation)$' -test.v -test.timeout=90s

docker run --name "$test_container" --network none --pid host --user 0:65534 \
  --sysctl net.ipv4.conf.lo.disable_xfrm=0 \
  --sysctl net.ipv6.conf.all.disable_ipv6=0 --sysctl net.ipv6.conf.lo.disable_ipv6=0 \
  --cap-drop ALL --cap-add NET_ADMIN --cap-add NET_BIND_SERVICE --cap-add SYS_NICE \
  --memory 512m --cpus 1 --security-opt no-new-privileges \
  --mount "type=volume,src=$runtime_volume,dst=/run/openvswitch,volume-nocopy,readonly" \
  --mount "type=bind,src=$runtime_binary,dst=/tmp/ipsec-runtime-tests,readonly" \
  --env KUBE_OVN_IPSEC_RUNTIME_TEST=true \
  "$candidate_image" /tmp/ipsec-runtime-tests -test.run '^TestCandidate(Runtime|MarkedGuard|DrainInventory)$' -test.v -test.timeout=180s
