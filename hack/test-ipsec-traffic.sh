#!/usr/bin/env bash
set -euo pipefail

# Synthetic transport traffic only. The IPsec processes use private network
# namespaces and the production capability set. Packet capture belongs to a
# separate fixture, which alone receives NET_RAW.
candidate_image=${1:?candidate image is required}
runtime_binary=$(realpath "${2:?compiled pkg/ipsec test binary is required}")
traffic_id="ipsec-traffic-${GITHUB_RUN_ID:-$$}-${GITHUB_RUN_ATTEMPT:-1}"
traffic_network="$traffic_id-net"
traffic_fixtures="$traffic_id-fixtures"
traffic_ovs=("$traffic_id-ovs-0" "$traffic_id-ovs-1")
traffic_tests=("$traffic_id-test-0" "$traffic_id-test-1")
traffic_sockets=("$traffic_id-socket-0" "$traffic_id-socket-1")

stop_case() {
  for i in 0 1; do
    docker logs "${traffic_tests[$i]}" 2>/dev/null || true
    docker logs "${traffic_ovs[$i]}" 2>/dev/null || true
    docker rm -f "${traffic_tests[$i]}" "${traffic_ovs[$i]}" >/dev/null 2>&1 || true
    docker volume rm "${traffic_sockets[$i]}" >/dev/null 2>&1 || true
  done
}
cleanup() {
  stop_case
  docker network rm "$traffic_network" >/dev/null 2>&1 || true
  docker volume rm "$traffic_fixtures" >/dev/null 2>&1 || true
}
trap cleanup EXIT

docker network create --internal --ipv6 --subnet "fd00:7598:$(printf '%x:%x' "$(( $$ / 65536 ))" "$(( $$ % 65536 ))")::/64" "$traffic_network" >/dev/null
docker volume create "$traffic_fixtures" >/dev/null
docker run --rm --network none --user 0:0 --cap-drop ALL --security-opt no-new-privileges \
  --mount "type=volume,src=$traffic_fixtures,dst=/fixtures" \
  --mount "type=bind,src=$runtime_binary,dst=/tmp/ipsec-tests,readonly" \
  --env KUBE_OVN_IPSEC_TRAFFIC_FIXTURES=true \
  "$candidate_image" /tmp/ipsec-tests -test.run '^TestCandidateTrafficFixtures$' -test.v

for family in 4 6; do
  for tunnel in geneve vxlan; do
    echo "Candidate transport: IPv$family $tunnel"
    docker run --rm --network none --user 0:0 --cap-drop ALL \
      --mount "type=volume,src=$traffic_fixtures,dst=/fixtures" \
      "$candidate_image" bash -c 'rm -f /fixtures/node-{0,1}/{finish,release,traffic-complete,guard-complete}'
    traffic_addresses=()
    for i in 0 1; do
      docker volume create "${traffic_sockets[$i]}" >/dev/null
      docker run --detach --name "${traffic_ovs[$i]}" --network "$traffic_network" --user 0:0 \
        --cap-drop ALL --cap-add NET_BIND_SERVICE --cap-add NET_RAW \
        --memory 256m --cpus 1 --security-opt no-new-privileges \
        --mount "type=volume,src=${traffic_sockets[$i]},dst=/run/openvswitch" \
        --mount "type=volume,src=$traffic_fixtures,dst=/fixtures" \
        "$candidate_image" bash -c '
          set -euo pipefail
          ovsdb-tool create /tmp/ipsec-test.db /usr/share/openvswitch/vswitch.ovsschema
          exec ovsdb-server /tmp/ipsec-test.db --remote=punix:/run/openvswitch/db.sock \
            --pidfile=/run/openvswitch/ovsdb-server.pid --unixctl=/run/openvswitch/db.ctl
        ' >/dev/null
      for attempt in {1..30}; do
        if docker exec "${traffic_ovs[$i]}" ovs-vsctl --timeout=1 --no-wait init; then
          break
        fi
        if [[ "$(docker inspect --format '{{.State.Running}}' "${traffic_ovs[$i]}")" != true || "$attempt" == 30 ]]; then
          echo 'Traffic OVSDB fixture failed' >&2
          exit 1
        fi
        sleep 1
      done
      docker exec "${traffic_ovs[$i]}" ovs-vsctl --timeout=5 --no-wait set Open_vSwitch . "external_ids:system-id=traffic-chassis-$i"
      if [[ "$family" == 4 ]]; then
        traffic_addresses+=("$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "${traffic_ovs[$i]}")")
      else
        traffic_addresses+=("$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.GlobalIPv6Address}}{{end}}' "${traffic_ovs[$i]}")")
      fi
    done
    docker exec --detach "${traffic_ovs[0]}" bash -c '
      set -euo pipefail
      tcpdump -Z root -i eth0 -p -n -U -w /tmp/traffic.pcap \
        "host $1 and (udp port 6081 or udp port 4789 or udp port 4500 or ip proto 50 or ip6 proto 50)" >/tmp/capture.log 2>&1 &
      traffic_capture_pid=$!
      echo "$traffic_capture_pid" >/tmp/capture.pid
      wait "$traffic_capture_pid"
      touch /tmp/capture-complete
    ' traffic-capture "${traffic_addresses[1]}"
    for attempt in {1..20}; do
      if docker exec "${traffic_ovs[0]}" bash -c 'grep -q "listening on eth0" /tmp/capture.log && kill -0 "$(cat /tmp/capture.pid)"'; then
        break
      fi
      if [[ "$attempt" == 20 ]]; then
        docker exec "${traffic_ovs[0]}" cat /tmp/capture.log >&2
        echo 'Packet capture did not start' >&2
        exit 1
      fi
      sleep 0.1
    done
    for i in 0 1; do
      peer=$((1-i))
      docker run --detach --name "${traffic_tests[$i]}" --network "container:${traffic_ovs[$i]}" --pid host --user 0:0 \
        --cap-drop ALL --cap-add NET_ADMIN --cap-add NET_BIND_SERVICE --cap-add SYS_NICE \
        --memory 512m --cpus 1 --security-opt no-new-privileges \
        --mount "type=volume,src=${traffic_sockets[$i]},dst=/run/openvswitch,readonly" \
        --mount "type=volume,src=$traffic_fixtures,dst=/fixtures,volume-subpath=node-$i" \
        --mount "type=bind,src=$runtime_binary,dst=/tmp/ipsec-tests,readonly" \
        --env KUBE_OVN_IPSEC_TRAFFIC_TEST=true --env "IPSEC_NODE=$i" --env "IPSEC_PEER_NODE=$peer" \
        --env "IPSEC_LOCAL_IP=${traffic_addresses[$i]}" --env "IPSEC_PEER_IP=${traffic_addresses[$peer]}" --env "IPSEC_TUNNEL=$tunnel" \
        "$candidate_image" /tmp/ipsec-tests -test.run '^TestCandidateEncryptedTraffic$' -test.v -test.timeout=180s >/dev/null
    done
    for attempt in {1..140}; do
      complete=true
      for i in 0 1; do
        if [[ "$(docker inspect --format '{{.State.Running}}' "${traffic_tests[$i]}")" != true ]]; then
          echo 'A candidate traffic runtime exited before successful exchange' >&2
          exit 1
        fi
        if ! docker exec "${traffic_ovs[$i]}" test -f "/fixtures/node-$i/traffic-complete"; then
          complete=false
        fi
      done
      if [[ "$complete" == true ]]; then
        break
      fi
      if [[ "$attempt" == 140 ]]; then
        echo 'Candidate encrypted transport exchange timed out' >&2
        exit 1
      fi
      sleep 1
    done
    for i in 0 1; do
      docker exec "${traffic_ovs[$i]}" touch "/fixtures/node-$i/finish"
    done
    for attempt in {1..40}; do
      complete=true
      for i in 0 1; do
        if [[ "$(docker inspect --format '{{.State.Running}}' "${traffic_tests[$i]}")" != true ]]; then
          echo 'A candidate exited before validating the persistent guard' >&2
          exit 1
        fi
        if ! docker exec "${traffic_ovs[$i]}" test -f "/fixtures/node-$i/guard-complete"; then
          complete=false
        fi
      done
      if [[ "$complete" == true ]]; then
        break
      fi
      if [[ "$attempt" == 40 ]]; then
        echo 'The guard did not survive runtime shutdown' >&2
        exit 1
      fi
      sleep 1
    done
    docker exec "${traffic_ovs[0]}" bash -c 'kill -INT "$(cat /tmp/capture.pid)"'
    for attempt in {1..20}; do
      if docker exec "${traffic_ovs[0]}" test -f /tmp/capture-complete; then
        break
      fi
      if [[ "$attempt" == 20 ]]; then
        docker exec "${traffic_ovs[0]}" cat /tmp/capture.log >&2
        echo 'Packet capture did not finish' >&2
        exit 1
      fi
      sleep 0.1
    done
    for i in 0 1; do
      docker exec "${traffic_ovs[$i]}" touch "/fixtures/node-$i/release"
      exit_code=$(docker wait "${traffic_tests[$i]}")
      if [[ "$exit_code" != 0 ]]; then
        echo "Candidate traffic test failed with status $exit_code" >&2
        exit 1
      fi
    done
    # tcpdump decodes both native ESP and ESP-in-UDP as ESP(spi=...). IKE on
    # UDP4500 carries a zero non-ESP marker and must not count as encrypted data.
    esp_count=$(docker exec "${traffic_ovs[0]}" bash -o pipefail -c 'tcpdump -Z root -n -r /tmp/traffic.pcap | awk "/ESP\\(spi=/{count++} END{print count+0}"')
    plaintext_count=$(docker exec "${traffic_ovs[0]}" bash -o pipefail -c 'tcpdump -Z root -n -r /tmp/traffic.pcap "udp port 6081 or udp port 4789" | wc -l')
    echo "IPv$family $tunnel: ESP packets=$esp_count plaintext transport packets=$plaintext_count"
    if [[ "$esp_count" == 0 || "$plaintext_count" != 0 ]]; then
      docker exec "${traffic_ovs[0]}" cat /tmp/capture.log >&2
      docker exec "${traffic_ovs[0]}" tcpdump -Z root -n -r /tmp/traffic.pcap -c 8 >&2
      echo 'The capture did not prove encrypted transport without plaintext' >&2
      exit 1
    fi
    stop_case
  done
done
