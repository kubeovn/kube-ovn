#!/usr/bin/env python3

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
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

    def runWithStubs(self, exitCode, interrupt=False):
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
    print('Validation of netpol-x/a -> netpol-y/b FAILED !!!', flush=True)
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
