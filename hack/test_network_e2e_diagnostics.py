#!/usr/bin/env python3

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parent))
import network_e2e_diagnostics as diagnostics


class NetworkDiagnosticsTest(unittest.TestCase):
    def testPodProjectionExcludesCredentialsAndKeepsNetworkIdentity(self):
        pod = {"kind": "Pod", "metadata": {"name": "a", "namespace": "netpol-x",
               "labels": {"pod": "a"}, "annotations": {"password": "private"}},
               "spec": {"nodeName": "worker", "containers": [{"env": [{"value": "private"}]}]},
               "status": {"podIP": "10.16.0.2"}}
        projected = diagnostics.networkObject(pod)
        self.assertNotIn("private", json.dumps(projected))
        self.assertEqual(projected["status"]["podIP"], "10.16.0.2")
        self.assertEqual(projected["spec"]["nodeName"], "worker")
        self.assertEqual(projected["metadata"]["labels"], {"pod": "a"})

    def testTimedOutCommandIsStoppedAndItsPartialOutputSurvives(self):
        result = diagnostics.capture([sys.executable, "-u", "-c",
                                      "import time; print('partial evidence'); time.sleep(10)"], seconds=0.1)
        self.assertNotEqual(result["returncode"], 0)
        self.assertIn("partial evidence", result["stdout"])
        self.assertIn("timed out", result["stderr"])

    def testFailureRetainsPreCleanupStateAndDoesNotMaskExitCode(self):
        self.runWithStubs(7)

    def testSuccessfulSuiteStillProducesReportsAndStopsWatchers(self):
        self.runWithStubs(0)

    def testInterruptedSuiteKeepsPartialArtifactsAndStopsChildProcesses(self):
        self.runWithStubs(143, interrupt=True)

    def testGinkgoImmediateFailureFreezesBeforeCleanup(self):
        self.runWithStubs(7, failureLine="[FAILED] Timed out after 120.000s.")

    def testRepeatedFailuresAreDebouncedAndEventsPersist(self):
        with tempfile.TemporaryDirectory() as temporary:
            collector = diagnostics.Diagnostics(Path(temporary), interval=0)

            def snapshot(directory, full=False, cancel=None):
                directory.mkdir(parents=True, exist_ok=True)
                index = snapshot.calls
                (directory / "marker.json").write_text(json.dumps({"index": index}))
                snapshot.calls += 1
                if index == 3:
                    collector.stop.set()

            snapshot.calls = 0
            collector.recordFailure("Validation fixture FAILED", "probe")
            collector.recordFailure("Validation fixture FAILED again", "probe")
            with mock.patch.object(diagnostics, "collectSnapshot", snapshot):
                collector.sample()
            windows = list(Path(temporary).glob("failures/*/preceding-snapshots.json"))
            self.assertEqual(len(windows), 1)
            events = [json.loads(line) for line in
                      (Path(temporary) / "failure-events.jsonl").read_text().splitlines()]
            self.assertEqual(len([event for event in events if event["phase"] == "observed"]), 2)
            snapshots = [event for event in events if event["phase"] == "snapshot"]
            self.assertEqual(len(snapshots), 1)
            self.assertEqual(snapshots[0]["snapshot"], "0")
            self.assertIn("snapshotStarted", snapshots[0])
            self.assertIn("snapshotFinished", snapshots[0])

    def testRuntimeCollectionOnlyTargetsBoundedTrafficDistributionClients(self):
        listing = "default client-private\ntraffic-distribution-1 server-0\n"
        listing += "".join(f"traffic-distribution-1 client-{i}\n" for i in range(10))
        with mock.patch.object(diagnostics, "podNames", return_value=["controller"]), \
                mock.patch.object(diagnostics, "capture", return_value={"stdout": listing}) as capture:
            jobs = diagnostics.runtimeJobs(["central"])
        clients = [command for name, command in jobs.items() if name.startswith("client-")]
        self.assertEqual(len(clients), 12)
        self.assertFalse(any("default" in command for command in clients))
        self.assertTrue(all("traffic-distribution-1" in command for command in clients))
        self.assertTrue(all("--tail=40" in command for command in clients if "logs" in command))
        self.assertIn('{"\\n"}', capture.call_args.args[0][-1])

    def testPreparationKeepsExistingOptionsAndEnablesNBFileDebugBeforeE2E(self):
        deployment = {"spec": {"template": {"spec": {"containers": [{
            "name": "kube-ovn-controller", "args": ["--np-enforcement=lax", "--v=2"],
            "env": [{"name": "PRIVATE", "value": "PRIVATE-FIXTURE"}],
        }]}}}}
        calls = []

        def capture(command, **kwargs):
            calls.append(command)
            return {"returncode": 0, "stdout": json.dumps(deployment) if "get" in command else "ok", "stderr": ""}

        with tempfile.TemporaryDirectory() as temporary, \
                mock.patch.object(diagnostics, "capture", capture), \
                mock.patch.object(diagnostics, "podNames", return_value=["central"]):
            self.assertEqual(diagnostics.prepareDebug(Path(temporary)), 0)
            for artifact in Path(temporary).glob("*.json"):
                self.assertNotIn("PRIVATE-FIXTURE", artifact.read_text())
        patchCall = next(command for command in calls if "patch" in command)
        container = json.loads(patchCall[-1])["spec"]["template"]["spec"]["containers"][0]
        self.assertIn("--np-enforcement=lax", container["args"])
        self.assertNotIn("--v=2", container["args"])
        self.assertIn("--enable-pprof=true", container["args"])
        self.assertEqual(container["env"], [{"name": "KUBE_OVN_LIBOVSDB_LOG_VERBOSITY", "value": "5"}])
        rolloutIndex = next(index for index, command in enumerate(calls) if "rollout" in command)
        vlogIndex = next(index for index, command in enumerate(calls) if "exec" in command)
        self.assertLess(rolloutIndex, vlogIndex)
        self.assertIn("vlog/set file:dbg", calls[vlogIndex][-1])

    def testPreparationFailurePreventsRunningAgainstAnUnreadyController(self):
        with tempfile.TemporaryDirectory() as temporary, \
                mock.patch.object(diagnostics, "capture", return_value={"returncode": 1, "stderr": "offline"}):
            self.assertEqual(diagnostics.prepareDebug(Path(temporary)), 1)

    def testIndependentClientProbeHasTotalTimeoutAndLeavesOriginalLoopAlone(self):
        socket = diagnostics.kubectl("exec", "-n", "traffic-distribution-1", "client-0", "--", "sh", "-c", "original socket probe")
        with mock.patch.object(diagnostics, "runtimeJobs", return_value={"client-test-sockets": socket}), \
                mock.patch.object(diagnostics, "podNames", return_value=[]):
            jobs = diagnostics.fastJobs()
        self.assertEqual(jobs["client-test-sockets"], socket)
        probe = jobs["client-test-independent-request"]
        self.assertEqual(probe[:-1], socket[:-1])
        self.assertIn("--max-time 3", probe[-1])
        self.assertIn("--connect-timeout 2", probe[-1])

    def testLiveStreamRotatesAndKeepsLateEvidenceWithinByteLimit(self):
        with tempfile.TemporaryDirectory() as temporary:
            collector = diagnostics.Diagnostics(Path(temporary), interval=1)
            collector.stream("fixture", [sys.executable, "-c", "import sys; sys.stdout.write('A' * 950 + 'LATE-EVIDENCE')"], maxBytes=100)
            files = list((Path(temporary) / "streams/fixture").glob("*.log"))
            self.assertEqual(len(files), 4)
            self.assertTrue(all(path.stat().st_size <= 100 for path in files))
            self.assertIn(b"LATE-EVIDENCE", b"".join(path.read_bytes() for path in files))
            self.assertEqual(collector.processes, [])

    def testRemoteStreamAndItsChildStopWhenCollectorCloses(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            shim = root / "kubectl"
            shim.write_text(f"#!{sys.executable}\n" + """import subprocess, sys
command = sys.argv[sys.argv.index('--') + 1:]
child = subprocess.Popen(command, start_new_session=True)
sys.exit(child.wait())
""")
            shim.chmod(0o755)
            collector = diagnostics.Diagnostics(root / "diagnostics", interval=1)
            command = ["kubectl", "exec", "fixture", "--", sys.executable, "-u", "-c",
                       "import time; print('remote-stream-fixture'); time.sleep(60)"]
            with mock.patch.dict(os.environ, {"PATH": f"{root}:{os.environ['PATH']}"}):
                thread = threading.Thread(target=collector.stream, args=("remote-fixture", command))
                collector.threads.append(thread)
                thread.start()
                deadline = time.monotonic() + 5
                log = root / "diagnostics/streams/remote-fixture/0.log"
                try:
                    while not log.exists() or b"remote-stream-fixture" not in log.read_bytes():
                        self.assertLess(time.monotonic(), deadline)
                        time.sleep(0.02)
                finally:
                    collector.close()
                self.assertFalse(thread.is_alive())
            processes = subprocess.check_output(["ps", "-eo", "args"], text=True)
            self.assertFalse(any("time.sleep(60)" in line and "remote-stream-fixture" in line for line in processes.splitlines()))
            cleanup = json.loads((root / "diagnostics/stream-cleanup.json").read_text())
            self.assertEqual(cleanup["remote-fixture"]["returncode"], 0)

    def testConfirmedFailureFreezesLiveLogsBeforeTheyRotate(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / "streams/nb/0.log"
            source.parent.mkdir(parents=True)
            source.write_text("pre-cleanup NB request")
            collector = diagnostics.Diagnostics(root, interval=1)
            collector.fastFailure.set()
            with mock.patch.object(diagnostics, "fastJobs", return_value={}), \
                    mock.patch.object(collector.fastFailure, "wait", side_effect=lambda _: collector.stop.set()):
                collector.fastSample()
            source.write_text("later test output")
            frozen = root / "probe-failures/0/streams/nb/0.log"
            self.assertEqual(frozen.read_text(), "pre-cleanup NB request")

    def runWithStubs(self, exitCode, interrupt=False,
                     failureLine="Validation of netpol-x/a -> netpol-y/b FAILED !!!"):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            binary = root / "bin"
            binary.mkdir()
            output = root / "diagnostics"
            pod = {"kind": "Pod", "metadata": {"name": "a", "namespace": "netpol-x",
                   "annotations": {"password": "PRIVATE-FIXTURE"}},
                   "spec": {"nodeName": "worker", "containers": [{"env": [{"value": "PRIVATE-FIXTURE"}]}]},
                   "status": {"podIP": "10.16.0.2"}}
            for name, source in {
                "kubectl": f'''import json, os, sys, time
from pathlib import Path
args = sys.argv[1:]
pod = {pod!r}
root = Path(os.environ['DIAGNOSTIC_TEST_ROOT'])
if '--watch' in args:
    print('fixture watch error', file=sys.stderr, flush=True)
    print(json.dumps({{'type': 'ADDED', 'object': pod}}) * 2, flush=True)
    time.sleep(60)
elif any('jsonpath=' in arg for arg in args):
    print('ovn-pod' if 'app=ovn-central' in args else 'ovs-pod')
elif 'exec' in args:
    print('partial OVS evidence')
    sys.exit(18)
elif 'json' in args:
    print(json.dumps({{'items': [] if (root / 'cleaned').exists() else [pod]}}))
else:
    print('node/worker')
''',
                "docker": "pass\n",
                "make": f'''import json, os, signal, sys, time
from pathlib import Path
root = Path(os.environ['DIAGNOSTIC_TEST_ROOT'])
output = root / 'diagnostics'
(root / 'make-args.json').write_text(json.dumps(sys.argv[1:]))
deadline = time.monotonic() + 10
while not (output / 'samples/0/pods.json').exists() or not (output / 'watch-pods.jsonl').exists():
    if time.monotonic() > deadline: sys.exit(99)
    time.sleep(0.02)
print('test output', flush=True)
if {exitCode}:
    print({failureLine!r}, flush=True)
    while not (output / 'failures/0/preceding-snapshots.json').exists():
        if time.monotonic() > deadline: sys.exit(98)
        time.sleep(0.02)
(root / 'cleaned').touch()
if {interrupt!r}:
    os.kill(os.getppid(), signal.SIGTERM)
    time.sleep(60)
sys.exit({exitCode})
''',
            }.items():
                path = binary / name
                path.write_text(f"#!{sys.executable}\n" + source)
                path.chmod(0o755)
            environment = {"PATH": f"{binary}:{os.environ['PATH']}", "DIAGNOSTIC_TEST_ROOT": str(root),
                           "E2E_SEED": "1234"}
            with mock.patch.dict(os.environ, environment):
                result = diagnostics.runSuite("k8s-netpol-e2e", output, interval=0.05)
            self.assertEqual(result, exitCode)
            metadata = json.loads((output / "metadata.json").read_text())
            self.assertFalse((output / "stop-collection-error.txt").exists())
            self.assertEqual(metadata["returncode"], exitCode)
            self.assertEqual(metadata["seed"], 1234)
            self.assertIn("test output", (output / "e2e.log").read_text())
            self.assertEqual(json.loads((output / "final/pods.json").read_text())["objects"], [])
            args = json.loads((root / "make-args.json").read_text())
            self.assertIn("--seed=1234", args[1])
            self.assertIn("--json-report=", args[1])
            self.assertIn("-v=5", args[2])
            self.assertIn("-disable-log-dump=true", args[2])
            self.assertIn("-dump-logs-on-failure=true", args[2])
            self.assertIn("-report-dir=", args[2])
            if exitCode:
                preceding = (output / "failures/0/preceding-snapshots.json").read_text()
                self.assertIn("10.16.0.2", preceding)
                self.assertIn("partial OVS evidence", preceding)
                events = [json.loads(line) for line in
                          (output / "failure-events.jsonl").read_text().splitlines()]
                self.assertTrue(any(event["phase"] == "observed" for event in events))
                self.assertTrue(any(event["phase"] == "snapshot" and
                                    event["snapshot"] == "0" for event in events))
            for artifact in output.rglob("*.json*"):
                self.assertNotIn("PRIVATE-FIXTURE", artifact.read_text())
            watches = [json.loads(line) for line in (output / "watch-pods.jsonl").read_text().splitlines()]
            self.assertEqual(len([event for event in watches if event.get("type") == "ADDED"]), 2)
            self.assertIn("fixture watch error", (output / "watch-pods.stderr.log").read_text())
            # The CLI stubs must not survive suite completion.
            processes = subprocess.check_output(["ps", "-eo", "args"], text=True)
            self.assertNotIn(f"{binary}/kubectl", processes)
            self.assertNotIn(f"{binary}/make", processes)


if __name__ == "__main__":
    unittest.main()
