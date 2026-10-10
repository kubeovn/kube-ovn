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
import subprocess
import threading
import time


resources = ("pods", "namespaces", "services", "endpointslices", "networkpolicies", "events")
failurePattern = re.compile(r"\[FAIL(?:ED)?\]|Unexpected endpoints|Validation of .* FAILED")


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
    except OSError as error:
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


def runtimeJobs(central, cancel=None):
    # Sample both ends of NB transactions to distinguish server/client starvation.
    runtime = """
date -u
ps -eo pid,comm,pcpu,pmem,stat,wchan
cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/cpu.stat /sys/fs/cgroup/memory.events
cat /sys/fs/cgroup/cpu.pressure /sys/fs/cgroup/io.pressure /sys/fs/cgroup/memory.pressure
cat /proc/net/tcp /proc/net/tcp6
for proc in /proc/[0-9]*; do
  case "$(cat "$proc/comm" 2>/dev/null)" in
    kube-ovn-contro*|ovsdb-server|ovn-northd)
      echo "=== $proc ==="
      cat "$proc/stat" "$proc/schedstat" "$proc/io"
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
        self.threads = []
        self.processes = []
        self.lock = threading.Lock()

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
            full = self.failure.is_set()
            self.failure.clear()
            snapshot = self.directory / "samples" / f"{index % 8}"
            collectSnapshot(snapshot, full=full, cancel=self.stop)
            # Freeze the pre-failure window before periodic collection overwrites it.
            history.append({p.name: p.read_text() for p in snapshot.glob("*.json")})
            if full:
                # Rotate windows: transient probe retries must not exhaust the budget.
                failureDir = self.directory / "failures" / str(failures % 8)
                failureDir.mkdir(parents=True, exist_ok=True)
                (failureDir / "preceding-snapshots.json").write_text(json.dumps(list(history)))
                failures += 1
            index += 1
            self.failure.wait(self.interval)

    def start(self):
        for target, args in [(self.watch, (resource,)) for resource in resources] + [(self.sample, ())]:
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
                        diagnostics.failure.set()
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
    parser.add_argument("action", choices=("run", "snapshot"))
    parser.add_argument("directory", type=Path)
    parser.add_argument("--suite", choices=("k8s-conformance-e2e", "k8s-netpol-e2e"))
    parser.add_argument("--interval", type=float, default=30)
    args = parser.parse_args()
    if args.interval <= 0:
        parser.error("interval must be positive")
    if args.action == "snapshot":
        collectSnapshot(args.directory, full=True)
        return 0
    if not args.suite:
        parser.error("run requires --suite")
    return runSuite(args.suite, args.directory, args.interval)


if __name__ == "__main__":
    raise SystemExit(main())
