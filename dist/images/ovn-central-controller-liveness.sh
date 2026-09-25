#!/usr/bin/env bash
# Liveness probe for ovn-central-controller (DYNAMIC_PEERS mode).
#
# The controller runs a dedicated goroutine that touches HEARTBEAT_FILE
# every ~5s, independent of the orchestration loop. The goroutine also
# self-suspends touches when ovsdb has been unhealthy past the
# OvsdbStuckTimeout / BootstrapLeaseDuration threshold (see startHeartbeat).
#
# A stale heartbeat file therefore signals one of:
#   - Go runtime hung (deadlock, frozen goroutines, OOM-spin).
#   - ovsdb-server unhealthy for >3 min outside recovery.
#   - recoverCluster() stuck >10 min (legitimate reconvert ceiling).
#
# Distinct from readiness, which only reflects "fully working cluster
# member": readiness drops legitimately during recovery and recovery is
# itself bounded by heartbeat-staleness rather than by readiness.

set -u

HEARTBEAT_FILE=${HEARTBEAT_FILE:-/var/run/ovn/ovn-central-controller.alive}
HEARTBEAT_STALE_THRESHOLD_SEC=${HEARTBEAT_STALE_THRESHOLD_SEC:-30}

if [[ ! -f "$HEARTBEAT_FILE" ]]; then
    # File hasn't been written yet. initialDelaySeconds on the probe
    # absorbs the cold-start window.
    echo "heartbeat file missing: $HEARTBEAT_FILE" >&2
    exit 1
fi

now=$(date +%s)
mtime=$(stat -c %Y "$HEARTBEAT_FILE" 2>/dev/null || echo 0)
age=$((now - mtime))
if (( age > HEARTBEAT_STALE_THRESHOLD_SEC )); then
    echo "heartbeat stale: age=${age}s threshold=${HEARTBEAT_STALE_THRESHOLD_SEC}s" >&2
    exit 1
fi
exit 0
