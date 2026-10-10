#!/usr/bin/env python3
"""Record networking test state before namespace cleanup removes the evidence."""

import argparse
import collections
import concurrent.futures
import datetime
import json
import os
from pathlib import Path
import re
import shlex
import signal
import shutil
import subprocess
import threading
import time


resources = ("pods", "namespaces", "services", "endpointslices", "networkpolicies", "events")
failurePattern = re.compile(r"\[FAIL(?:ED)?\]|Unexpected endpoints|Validation of .* FAILED")
terminalFailurePattern = re.compile(r"(?:\[(?:FAIL|FAILED)\] in \[It\]|\[FAIL\] \[|^FAIL!|--- FAIL)")
failureSnapshotCooldown = 30
fastFailureCooldown = 10


def timestamp():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def networkObject(obj):
    """Keep network state, excluding Pod environments, volumes and annotations."""
    metadata = obj.get("metadata", {})
    result = {"kind": obj.get("kind"), "metadata": {
        key: metadata[key] for key in (
            "name", "namespace", "uid", "resourceVersion", "labels", "deletionTimestamp",
        ) if key in metadata
    }}
    if obj.get("kind") == "Pod":
        result["spec"] = {key: obj.get("spec", {}).get(key) for key in ("nodeName", "hostNetwork")}
        result["status"] = {key: obj.get("status", {}).get(key) for key in (
            "phase", "podIP", "podIPs", "conditions", "containerStatuses",
        )}
    else:
        for key in ("spec", "status", "addressType", "endpoints", "ports", "message",
                    "reason", "involvedObject", "firstTimestamp", "lastTimestamp", "count"):
            if key in obj:
                result[key] = obj[key]
    return result


def capture(command, seconds=10, cancel=None):
    try:
        process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                                   text=True, start_new_session=True)
    except (OSError, ValueError) as error:
        return {"started": timestamp(), "command": command, "error": str(error)}
    started = timestamp()
    deadline = time.monotonic() + seconds
    while True:
        try:
            stdout, stderr = process.communicate(timeout=0.2)
            break
        except subprocess.TimeoutExpired:
            if time.monotonic() >= deadline or (cancel is not None and cancel.is_set()):
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                stdout, stderr = process.communicate()
                stderr += "\nDiagnostic command timed out or collection was stopped\n"
                break
    return {"started": started, "finished": timestamp(), "command": command,
            "returncode": process.returncode, "stdout": stdout, "stderr": stderr}


def kubectl(*args):
    return ["kubectl", "--request-timeout=5s", *args]


def podNames(selector, cancel=None):
    result = capture(kubectl("get", "pods", "-n", "kube-system", "-l", selector,
                             "-o", "jsonpath={.items[*].metadata.name}"), cancel=cancel)
    return result.get("stdout", "").split()


def prepareDebug(directory):
    """Enable diagnostics before E2E; wait for the controller restart to finish."""
    directory.mkdir(parents=True, exist_ok=True)
    result = capture(kubectl("get", "deployment", "kube-ovn-controller", "-n", "kube-system", "-o", "json"))
    try:
        containers = json.loads(result.get("stdout", "")).get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
        controller = next(container for container in containers if container["name"] == "kube-ovn-controller")
    except (ValueError, StopIteration, KeyError):
        (directory / "controller-error.json").write_text(json.dumps({"error": "cannot read controller deployment", "stderr": result.get("stderr")}))
        return 1
    flags = ("--v=5", "--vmodule=network_policy=6,endpoint_slice=6,ovn*=6", "--enable-pprof=true", "--pprof-port=10660")
    args = [arg for arg in controller.get("args", [])
            if not arg.startswith(("--v=", "--vmodule=", "--enable-pprof=", "--pprof-port="))]
    patch = {"spec": {"template": {"spec": {"containers": [{"name": "kube-ovn-controller",
        "args": args + list(flags), "env": [{"name": "KUBE_OVN_LIBOVSDB_LOG_VERBOSITY", "value": "5"}]}]}}}}
    result = capture(kubectl("patch", "deployment", "kube-ovn-controller", "-n", "kube-system",
                             "--type=strategic", "-p", json.dumps(patch)))
    result.pop("command", None)
    result["debugFlags"] = flags
    (directory / "controller-patch.json").write_text(json.dumps(result, indent=2))
    if result.get("returncode") != 0:
        return 1
    rollout = capture(["kubectl", "--request-timeout=185s", "rollout", "status",
        "deployment/kube-ovn-controller", "-n", "kube-system", "--timeout=180s"], seconds=185)
    (directory / "controller-rollout.json").write_text(json.dumps(rollout, indent=2))
    if rollout.get("returncode") != 0:
        return 1
    # All NB modules log to file at debug level; stderr/console levels stay unchanged.
    failed = False
    central = podNames("app=ovn-central")
    for pod in central:
        result = capture(kubectl("exec", "-n", "kube-system", pod, "-c", "ovn-central", "--", "sh", "-ec", '''
date -u
echo '=== NB vlog before ==='
ovs-appctl -T 2 -t /var/run/ovn/ovnnb_db.ctl vlog/list
ovs-appctl -T 2 -t /var/run/ovn/ovnnb_db.ctl vlog/set file:dbg
echo '=== NB vlog after ==='
ovs-appctl -T 2 -t /var/run/ovn/ovnnb_db.ctl vlog/list
'''))
        (directory / f"nb-vlog-{pod}.json").write_text(json.dumps(result, indent=2))
        failed |= result.get("returncode") != 0
    for pod in podNames("app=ovs"):
        result = capture(kubectl("exec", "-n", "kube-system", pod, "-c", "openvswitch", "--", "sh", "-ec", '''
for socket in /var/run/ovn/ovn-controller*.ctl; do
  levels=$(ovs-appctl -T 2 -t "$socket" vlog/list)
  echo "=== $socket before ==="
  printf '%s\n' "$levels"
  for module in binding lflow ofctrl reconnect; do
    if printf '%s\n' "$levels" | awk -v m="$module" '$1==m {found=1} END {exit !found}'; then
      ovs-appctl -T 2 -t "$socket" vlog/set "$module:file:dbg"
    fi
  done
  echo "=== $socket after ==="
  ovs-appctl -T 2 -t "$socket" vlog/list
done
'''))
        # Older scheduled images may use different sockets/modules; retain the result.
        (directory / f"ovn-controller-vlog-{pod}.json").write_text(json.dumps(result, indent=2))
    return int(failed or not central)


def runtimeJobs(central, cancel=None):
    # Sample both ends of NB transactions to distinguish server/client starvation.
    runtime = """
date -u
ps -eo pid,comm,pcpu,pmem,stat,wchan
cat /proc/uptime /proc/loadavg
cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/memory.events
cat /sys/fs/cgroup/cpu.pressure /sys/fs/cgroup/io.pressure /sys/fs/cgroup/memory.pressure
cat /proc/net/tcp /proc/net/tcp6
for proc in /proc/[0-9]*; do
  case "$(cat "$proc/comm" 2>/dev/null)" in
    kube-ovn-contro*|ovsdb-server|ovn-northd)
      echo "=== $proc ==="
      cat "$proc/stat" "$proc/schedstat" "$proc/io"
      for task in "$proc"/task/*; do
        echo "=== $task ==="
        cat "$task/stat" "$task/schedstat" "$task/wchan"
      done
      ;;
  esac
done
"""
    jobs = {}
    for selector, container, pods in (
        ("central", "ovn-central", central),
        ("controller", "kube-ovn-controller", podNames("app=kube-ovn-controller", cancel)),
    ):
        for pod in pods[:3]:
            jobs[f"runtime-{selector}-{pod}"] = kubectl(
                "exec", "-n", "kube-system", pod, "-c", container, "--", "sh", "-c", runtime)
    # The PreferSameNode probe uses sequential curl without a request deadline.
    # Capture only those clients, with bounded output, while endpoints still exist.
    result = capture(kubectl("get", "pods", "-A", "-o",
        'jsonpath={range .items[*]}{.metadata.namespace}{" "}{.metadata.name}{"\\n"}{end}'), cancel=cancel)
    clients = [line.split() for line in result.get("stdout", "").splitlines()]
    clients = [pair for pair in clients if len(pair) == 2
               and pair[0].startswith("traffic-distribution-") and pair[1].startswith("client-")]
    for namespace, pod in clients[:6]:
        name = f"client-{namespace}-{pod}"
        jobs[f"{name}-logs"] = kubectl("logs", "-n", namespace, pod, "--timestamps", "--tail=40")
        jobs[f"{name}-sockets"] = kubectl("exec", "-n", namespace, pod, "--", "sh", "-c",
            "date -u; ps -eo pid,comm,stat,wchan; cat /proc/net/tcp /proc/net/tcp6")
    return jobs


def fastJobs(cancel=None):
    jobs = runtimeJobs([], cancel)
    for pod in podNames("app=kube-ovn-controller", cancel)[:3]:
        jobs[f"goroutines-{pod}"] = kubectl("exec", "-n", "kube-system", pod, "-c", "kube-ovn-controller", "--",
            "curl", "-q", "-fsS", "--max-time", "2", "--max-filesize", "1048576", "http://127.0.0.1:10660/debug/pprof/goroutine?debug=2")
        # Bypass libovsdb using the controller's network namespace and configured NB endpoint.
        jobs[f"nb-controller-{pod}"] = kubectl("exec", "-n", "kube-system", pod, "-c", "kube-ovn-controller", "--", "sh", "-ec", '''
date -u
if [ "${ENABLE_SSL:-false}" = true ]; then echo 'TCP probe unavailable with SSL' >&2; exit 1; fi
address="${OVN_NB_ADDR:-}"
if [ -z "$address" ]; then
  host="${OVN_DB_IPS:-${OVN_NB_SERVICE_HOST:-}}"
  host="${host%%,*}"
  address="tcp:[$host]:${KUBE_OVN_NB_PORT:-6641}"
fi
timeout 3 ovsdb-client --timeout=2 query "${address%%,*}" '["OVN_Northbound",{"op":"select","table":"NB_Global","where":[],"columns":["nb_cfg"]}]'
''')
    for pod in podNames("app=ovn-central", cancel)[:3]:
        jobs[f"nb-local-{pod}"] = kubectl("exec", "-n", "kube-system", pod, "-c", "ovn-central", "--",
            "ovsdb-client", "--timeout=2", "query", "unix:/var/run/ovn/ovnnb_db.sock",
            '["OVN_Northbound",{"op":"select","table":"NB_Global","where":[],"columns":["nb_cfg"]}]')
    for name, command in list(jobs.items()):
        if name.startswith("client-") and name.endswith("-sockets"):
            jobs[name.removesuffix("-sockets") + "-independent-request"] = command[:-1] + [
                "date -u; curl -q -v -sS --connect-timeout 2 --max-time 3 http://traffic-dist-test-service:80/"]
    return jobs


def collectSnapshot(directory, full=False, cancel=None):
    directory.mkdir(parents=True, exist_ok=True)
    for old in directory.glob("*.json"):
        old.unlink()
    jobs = {resource: kubectl("get", resource, "-A", "-o", "json") for resource in resources}
    jobs["nodes"] = kubectl("get", "nodes", "-o", "wide")
    central = podNames("app=ovn-central", cancel)
    if central:
        tables = "NB_Global Load_Balancer ACL Address_Set Port_Group"
        if full:
            tables += " Logical_Switch Logical_Switch_Port"
        jobs["ovn-nb"] = kubectl("exec", "-n", "kube-system", central[0], "-c", "ovn-central", "--", "sh", "-c",
            'for table in "$@"; do echo "=== $table ==="; '
            'timeout 3 ovn-nbctl --timeout=2 list "$table"; done', "sh", *tables.split())
        jobs["ovn-sb"] = kubectl("exec", "-n", "kube-system", central[0], "-c", "ovn-central", "--", "sh", "-c",
            'for table in SB_Global Chassis_Private Chassis_Template_Var Port_Binding; do echo "=== $table ==="; '
            'timeout 3 ovn-sbctl --timeout=2 list "$table"; done')
        jobs["ovn-log-rotation"] = kubectl("exec", "-n", "kube-system", central[0], "-c", "ovn-central", "--",
            "logrotate", "--state", "/tmp/network-e2e-logrotate.status", "/etc/logrotate.d/ovn")
    jobs.update(runtimeJobs(central, cancel))
    for pod in podNames("app=ovs", cancel):
        # Cookie zero includes learned affinity flows; full snapshots also record ACL flows.
        flowFilter = "" if full else "cookie=0x0/-1"
        jobs[f"ovs-{pod}"] = kubectl("exec", "-n", "kube-system", pod, "-c", "openvswitch",
            "--", "sh", "-c", '''
date -u
id
echo '=== interfaces and ofports ==='
timeout 3 ovs-vsctl --timeout=2 show
timeout 3 ovs-vsctl --timeout=2 --columns=name,ofport,error,external_ids list Interface
echo '=== OpenFlow and learned affinity flows ==='
timeout 3 ovs-ofctl -O OpenFlow13 dump-flows br-int "$1"
echo '=== datapath ==='
timeout 3 ovs-appctl -T 2 dpif/show
echo '=== conntrack ==='
timeout 3 ovs-appctl -T 2 dpctl/dump-conntrack
echo '=== coverage ==='
timeout 3 ovs-appctl -T 2 coverage/show
echo '=== OVS cgroup throttling and memory pressure ==='
cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/memory.events
''', "sh", flowFilter)
    if full:
        result = capture(["docker", "ps", "--filter", "label=io.x-k8s.kind.cluster=kube-ovn",
                          "--format", "{{.Names}}"], cancel=cancel)
        for node in result.get("stdout", "").split():
            jobs[f"node-{node}"] = ["docker", "exec", node, "sh", "-c", '''
date -u
ip -details addr
ip route
ip -6 route
ip neigh
cat /proc/loadavg
cat /proc/meminfo
echo '=== cgroup CPU throttling and memory pressure ==='
cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/memory.events
journalctl -u kubelet -u containerd --since '-10 minutes' --no-pager -n 1000
''']
    # A broken API/OVS command must not stop independent evidence collection.
    def save(item):
        name, command = item
        result = capture(command, seconds=25, cancel=cancel)
        if name in resources:
            raw = result.pop("stdout", "")
            try:
                result["objects"] = [networkObject(obj) for obj in json.loads(raw).get("items", [])]
            except (json.JSONDecodeError, AttributeError) as error:
                result["decodeError"] = str(error)
        (directory / f"{name}.json").write_text(json.dumps(result, indent=2) + "\n")
    with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
        list(executor.map(save, jobs.items()))


class Diagnostics:
    def __init__(self, directory, interval):
        self.directory = directory
        self.interval = interval
        self.stop = threading.Event()
        self.failure = threading.Event()
        self.fastFailure = threading.Event()
        self.threads = []
        self.processes = []
        self.remoteStreams = []
        self.lock = threading.Lock()
        self.failureStateLock = threading.Lock()
        self.failureSequence = 0
        self.pendingFailure = None
        self.pendingFastFailure = None
        self.lastFailureSnapshot = 0.0
        self.lastFastFailure = 0.0

    def recordFailure(self, line, kind):
        """Record every failure line, while sampling only a bounded subset."""
        with self.failureStateLock:
            self.failureSequence += 1
            event = {"phase": "observed", "sequence": self.failureSequence,
                     "observed": timestamp(), "kind": kind,
                     "line": line.rstrip()[:8192], "snapshot": None,
                     "snapshotStarted": None, "snapshotFinished": None}
            self.pendingFailure = event
            self.pendingFastFailure = event
            with (self.directory / "failure-events.jsonl").open("a") as output:
                output.write(json.dumps(event) + "\n")
        self.failure.set()
        if kind == "test":
            self.fastFailure.set()

    def _consumeFailure(self, fast=False):
        event = None
        now = time.monotonic()
        with self.failureStateLock:
            marker = self.lastFastFailure if fast else self.lastFailureSnapshot
            cooldown = fastFailureCooldown if fast else failureSnapshotCooldown
            signal = self.fastFailure if fast else self.failure
            pending = self.pendingFastFailure if fast else self.pendingFailure
            if signal.is_set():
                signal.clear()
                if pending is not None and now - marker >= cooldown:
                    event = pending
                    if fast:
                        self.pendingFastFailure = None
                    else:
                        self.pendingFailure = None
                    if fast:
                        self.lastFastFailure = now
                    else:
                        self.lastFailureSnapshot = now
                elif pending is None and now - marker >= cooldown:
                    event = {"phase": "signal", "observed": timestamp(),
                             "kind": "unknown", "line": None}
                elif pending is not None:
                    if fast:
                        self.pendingFastFailure = None
                    else:
                        self.pendingFailure = None
        return event

    def _recordFailureSnapshot(self, event, directory, started, finished, error=None):
        record = dict(event or {})
        record.update({"phase": "snapshot", "snapshot": str(directory),
                       "snapshotStarted": started, "snapshotFinished": finished})
        if error:
            record["error"] = str(error)
        with self.failureStateLock:
            with (self.directory / "failure-events.jsonl").open("a") as output:
                output.write(json.dumps(record) + "\n")

    def stream(self, name, command, maxBytes=4 * 1024 * 1024):
        """Retain four bounded segments of live logs, including packet headers only."""
        directory = self.directory / "streams" / name
        directory.mkdir(parents=True, exist_ok=True)
        cleanup = None
        if "exec" in command and command[0] == "kubectl":
            offset = command.index("--")
            pidfile = f"/tmp/network-e2e-diagnostic-{os.getpid()}-{name}.pid"
            prefix = command[:offset + 1]
            command = prefix + ["sh", "-c", r'''
pidfile="$1"
shift
printf '%s\n' "$$" > "$pidfile"
child=''
trap 'if [ -n "$child" ]; then kill -TERM "$child" 2>/dev/null || true; fi; rm -f "$pidfile"' EXIT
trap 'exit 143' TERM INT
timeout -s TERM 10800 "$@" &
child=$!
wait "$child"
''', "sh", pidfile, *command[offset + 1:]]
            cleanup = prefix + ["sh", "-c", r'''
if [ -f "$1" ]; then
  read -r pid < "$1"
  if tr '\000' '\n' < "/proc/$pid/cmdline" 2>/dev/null | grep -Fxq "$1"; then
    kill -TERM "$pid"
  fi
fi
''', "sh", pidfile]
        with self.lock:
            if self.stop.is_set():
                return
            process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                       bufsize=0, start_new_session=True)
            self.processes.append(process)
            if cleanup:
                self.remoteStreams.append((name, cleanup))
        metadata = {"started": timestamp(), "command": command, "segments": 4, "maxSegmentBytes": maxBytes}
        (directory / "metadata.json").write_text(json.dumps(metadata, indent=2))
        index = 0
        written = 0
        output = (directory / "0.log").open("wb")
        try:
            with process.stdout:
                while chunk := process.stdout.read(65536):
                    while chunk:
                        count = min(len(chunk), maxBytes - written)
                        output.write(chunk[:count])
                        output.flush()
                        written += count
                        chunk = chunk[count:]
                        if written == maxBytes:
                            output.close()
                            index += 1
                            metadata["lastSegment"] = index
                            (directory / "metadata.json").write_text(json.dumps(metadata, indent=2))
                            written = 0
                            output = (directory / f"{index % 4}.log").open("wb")
            process.wait()
        finally:
            output.close()
            if process.poll() is None:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                process.wait()
            metadata.update(finished=timestamp(), returncode=process.poll(), lastSegment=index)
            (directory / "metadata.json").write_text(json.dumps(metadata, indent=2))
            with self.lock:
                self.processes.remove(process)

    def liveLogs(self):
        targets = []
        for pod in podNames("app=kube-ovn-controller", self.stop)[:3]:
            targets.append((f"controller-{pod}", ["kubectl", "--request-timeout=0", "logs", "-f",
                "-n", "kube-system", pod, "-c", "kube-ovn-controller", "--timestamps", "--tail=100"]))
        for pod in podNames("app=ovn-central", self.stop)[:3]:
            prefix = ["kubectl", "--request-timeout=0", "exec", "-n", "kube-system", pod, "-c", "ovn-central", "--"]
            targets.append((f"nb-file-{pod}", prefix + ["tail", "-n", "100", "-F", "/var/log/ovn/ovsdb-server-nb.log"]))
            targets.append((f"db-packet-headers-{pod}", prefix + ["tcpdump", "-i", "any", "-p", "-nn", "-tttt", "-S", "-l",
                "tcp port 6641 or tcp port 6642 or tcp port 6643 or tcp port 6644"]))
        for pod in podNames("app=ovs", self.stop):
            prefix = ["kubectl", "--request-timeout=0", "exec", "-n", "kube-system", pod, "-c", "openvswitch", "--"]
            targets.append((f"ovn-controller-file-{pod}", prefix + ["tail", "-n", "100", "-F", "/var/log/ovn/ovn-controller.log"]))
            targets.append((f"service-packet-headers-{pod}", prefix + ["tcpdump", "-i", "any", "-p", "-nn", "-tttt", "-S", "-l",
                "tcp port 80 or tcp port 81 or tcp port 9376"]))
        for name, command in targets:
            thread = threading.Thread(target=self.guard, args=(self.stream, (name, command)), daemon=True)
            with self.lock:
                if self.stop.is_set():
                    break
                self.threads.append(thread)
                thread.start()

    def fastSample(self):
        history = collections.deque(maxlen=12)
        index = 0
        failures = 0
        while not self.stop.is_set():
            event = self._consumeFailure(fast=True)
            if event:
                failureDir = self.directory / "probe-failures" / str(failures % 8)
                failureDir.mkdir(parents=True, exist_ok=True)
                for source in (self.directory / "streams").glob("*/*"):
                    destination = failureDir / "streams" / source.parent.name / source.name
                    destination.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(source, destination)
            jobs = fastJobs(self.stop)
            with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
                results = list(executor.map(lambda item: (item[0], capture(item[1], seconds=6, cancel=self.stop)), jobs.items()))
            sample = dict(results)
            directory = self.directory / "probes"
            directory.mkdir(parents=True, exist_ok=True)
            (directory / f"{index % 12}.json").write_text(json.dumps(sample))
            history.append(sample)
            if event:
                (failureDir / "preceding-probes.json").write_text(json.dumps(list(history)))
                failures += 1
            index += 1
            self.fastFailure.wait(5)

    def watch(self, resource):
        # Reconnect watches after API errors, retaining timestamps and resourceVersion.
        with (self.directory / f"watch-{resource}.jsonl").open("w") as output, \
                (self.directory / f"watch-{resource}.stderr.log").open("w") as errors:
            while not self.stop.is_set():
                with self.lock:
                    if self.stop.is_set():
                        break
                    process = subprocess.Popen(["kubectl", "--request-timeout=0", "get",
                        resource, "-A", "--watch", "--output-watch-events", "-o", "json"],
                        stdout=subprocess.PIPE, stderr=errors,
                        text=True, start_new_session=True)
                    self.processes.append(process)
                errors.write(f"{timestamp()} watch connected pid={process.pid}\n")
                errors.flush()
                output.write(json.dumps({"observed": timestamp(), "watch": "connected"}) + "\n")
                output.flush()
                buffer = ""
                decoder = json.JSONDecoder()
                with process.stdout:
                    for line in process.stdout:
                        buffer += line
                        while buffer.strip():
                            buffer = buffer.lstrip()
                            try:
                                obj, offset = decoder.raw_decode(buffer)
                            except json.JSONDecodeError:
                                break
                            buffer = buffer[offset:]
                            record = {"observed": timestamp(), "type": obj.get("type"),
                                      "object": networkObject(obj.get("object", {}))}
                            output.write(json.dumps(record) + "\n")
                            output.flush()
                process.wait()
                with self.lock:
                    self.processes.remove(process)
                output.write(json.dumps({"observed": timestamp(), "watch": "disconnected",
                                         "returncode": process.returncode}) + "\n")
                output.flush()
                self.stop.wait(3)

    def sample(self):
        history = collections.deque(maxlen=8)
        index = 0
        failures = 0
        while not self.stop.is_set():
            event = self._consumeFailure()
            full = event is not None
            snapshot = self.directory / "samples" / f"{index % 8}"
            snapshotStarted = timestamp()
            snapshotError = None
            try:
                collectSnapshot(snapshot, full=full, cancel=self.stop)
            except Exception as error:
                snapshotError = error
            snapshotFinished = timestamp()
            # Freeze the pre-failure window before periodic collection overwrites it.
            history.append({p.name: p.read_text() for p in snapshot.glob("*.json")})
            if full:
                # Rotate windows: transient probe retries must not exhaust the budget.
                failureDir = self.directory / "failures" / str(failures % 8)
                failureDir.mkdir(parents=True, exist_ok=True)
                (failureDir / "preceding-snapshots.json").write_text(json.dumps(list(history)))
                self._recordFailureSnapshot(event, failureDir.name, snapshotStarted,
                                            snapshotFinished, snapshotError)
                failures += 1
            index += 1
            self.failure.wait(self.interval)

    def start(self):
        for target, args in [(self.watch, (resource,)) for resource in resources] + [
                (self.sample, ()), (self.fastSample, ()), (self.liveLogs, ())]:
            thread = threading.Thread(target=self.guard, args=(target, args), daemon=True)
            thread.start()
            self.threads.append(thread)

    def guard(self, target, args):
        try:
            target(*args)
        except Exception as error:
            with self.lock:
                with (self.directory / "collection-errors.jsonl").open("a") as output:
                    output.write(json.dumps({"observed": timestamp(), "collector": target.__name__,
                                             "error": str(error)}) + "\n")

    def close(self):
        self.stop.set()
        self.failure.set()
        self.fastFailure.set()
        # Closing kubectl's transport alone may leave remote tail/tcpdump running.
        with self.lock:
            remote = list(self.remoteStreams)
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as executor:
            results = list(executor.map(lambda item: (item[0], capture(item[1], seconds=3)), remote))
        (self.directory / "stream-cleanup.json").write_text(json.dumps(dict(results), indent=2))
        with self.lock:
            for process in self.processes:
                try:
                    os.killpg(process.pid, signal.SIGTERM)
                except ProcessLookupError:
                    pass
        deadline = time.monotonic() + 10
        for thread in self.threads:
            thread.join(timeout=max(0, deadline - time.monotonic()))
        with self.lock:
            for process in self.processes:
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
        for thread in self.threads:
            thread.join(timeout=1)


def runSuite(suite, directory, interval):
    directory = directory.resolve()
    directory.mkdir(parents=True, exist_ok=True)
    seed = int(os.environ.get("E2E_SEED", str(int(time.time()))))
    metadata = {"started": timestamp(), "seed": seed, "suite": suite,
                "head": os.environ.get("EXECUTION_SHA", os.environ.get("GITHUB_SHA")), "run": os.environ.get("GITHUB_RUN_ID"),
                "ipFamily": os.environ.get("E2E_IP_FAMILY"),
                "networkMode": os.environ.get("E2E_NETWORK_MODE"),
                "enforcement": os.environ.get("NP_ENFORCEMENT")}
    (directory / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
    diagnostics = Diagnostics(directory, interval)
    diagnostics.start()
    count = capture(kubectl("get", "nodes", "-o", "name")).get("stdout", "").split()
    command = ["make", suite,
        "GINKGO_OUTPUT_OPT=--github-output --silence-skips --no-color --show-node-events "
        f"--seed={seed} --poll-progress-after=30s --poll-progress-interval=30s "
        f"--json-report={shlex.quote(str(directory / 'ginkgo.json'))} "
        f"--junit-report={shlex.quote(str(directory / 'junit.xml'))}",
        f"TEST_BIN_ARGS=-kubeconfig {shlex.quote(os.environ.get('KUBECONFIG', str(Path.home() / '.kube/config')))} "
        # Kubernetes repo-only log-dump.sh is unavailable; retain API-based failure dumps.
        f"-disable-log-dump=true -dump-logs-on-failure=true "
        f"-num-nodes {len(count)} -report-dir={shlex.quote(str(directory / 'kubernetes'))} -v=5"]
    process = None
    interrupted = 0
    killTimer = None

    def interrupt(signum, frame):
        nonlocal interrupted, killTimer
        interrupted = signum
        diagnostics.stop.set()
        if process is not None and process.poll() is None:
            def kill():
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            killTimer = threading.Timer(5, kill)
            killTimer.daemon = True
            killTimer.start()

    handlers = {sig: signal.signal(sig, interrupt) for sig in (signal.SIGTERM, signal.SIGINT)}
    result = 127
    try:
        with (directory / "e2e.log").open("w") as log:
            process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                       text=True, bufsize=1, start_new_session=True)
            with process.stdout:
                for line in process.stdout:
                    print(line, end="", flush=True)
                    log.write(line)
                    log.flush()
                    if failurePattern.search(line):
                        kind = "test" if terminalFailurePattern.search(line) else "probe"
                        diagnostics.recordFailure(line, kind)
            result = process.wait()
    except OSError as error:
        (directory / "suite-start-error.txt").write_text(str(error))
    finally:
        if process is not None and process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
        if killTimer is not None:
            killTimer.cancel()
        for sig, handler in handlers.items():
            signal.signal(sig, handler)
        try:
            diagnostics.close()
        except Exception as error:
            (directory / "stop-collection-error.txt").write_text(str(error))
    if interrupted:
        result = 128 + interrupted
    try:
        collectSnapshot(directory / "final", full=True,
                        cancel=diagnostics.stop if interrupted else None)
    except Exception as error:
        (directory / "final-collection-error.txt").write_text(str(error))
    metadata.update({"finished": timestamp(), "returncode": result})
    (directory / "metadata.json").write_text(json.dumps(metadata, indent=2) + "\n")
    return result


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("prepare", "run", "snapshot"))
    parser.add_argument("directory", type=Path)
    parser.add_argument("--suite", choices=("k8s-conformance-e2e", "k8s-netpol-e2e"))
    parser.add_argument("--interval", type=float, default=30)
    args = parser.parse_args()
    if args.interval <= 0:
        parser.error("interval must be positive")
    if args.action == "prepare":
        return prepareDebug(args.directory)
    if args.action == "snapshot":
        collectSnapshot(args.directory, full=True)
        return 0
    if not args.suite:
        parser.error("run requires --suite")
    return runSuite(args.suite, args.directory, args.interval)


if __name__ == "__main__":
    raise SystemExit(main())
