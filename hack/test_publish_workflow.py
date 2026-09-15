#!/usr/bin/env python3
"""Regression tests for the multi-arch assembly in .github/workflows/publish.yaml.

The workflow pulls both architecture tags, checks that they were built from the
same revision and assembles the manifest list from the digests of the images it
verified. The shell block is extracted from the workflow and executed with a stub
docker, so the decision table (publish, skip, fail) stays covered without a
registry or an image pull.
"""

import os
import re
import shutil
import subprocess
import tempfile
import unittest
from pathlib import Path

repoRoot = Path(__file__).resolve().parent.parent
workflowPath = repoRoot / ".github/workflows/publish.yaml"
# The workflow reads the tag from the checked-in VERSION, so the fake selectors
# below must use the same value instead of a hard-coded release.
imageTag = (repoRoot / "VERSION").read_text().strip()


def dockerRef(repository, suffix=""):
    return f"{repository}:{imageTag}{suffix}"


def revisionEnvVar(ref):
    return "FAKE_REV_" + re.sub(r"[^A-Za-z0-9]", "_", ref)

DOCKER_STUB = """#!/bin/bash
log() { echo "$@" >> "$FAKE_DOCKER_LOG"; }
case "$1" in
  login) cat >/dev/null; exit 0 ;;
  pull)
    ref="$2"
    if [ -n "${FAKE_PULL_MISSING:-}" ] && [[ "$FAKE_PULL_MISSING" == *"$ref"* ]]; then
      echo "Error response from daemon: manifest for $ref not found: manifest unknown: manifest unknown" >&2
      exit 1
    fi
    if [ -n "${FAKE_PULL_ERROR:-}" ] && [[ "$FAKE_PULL_ERROR" == *"$ref"* ]]; then
      echo "${FAKE_PULL_ERROR_MSG:-Error response from daemon: unauthorized: authentication required}" >&2
      exit 1
    fi
    exit 0 ;;
  image)
    format="$4"
    ref="$5"
    if [ -n "${FAKE_INSPECT_FAIL:-}" ] && [[ "$FAKE_INSPECT_FAIL" == *"$ref"* ]]; then
      echo "Error response from daemon: cannot inspect image" >&2
      exit 1
    fi
    key=$(printf '%s' "$ref" | tr -c 'A-Za-z0-9' '_')
    if [[ "$format" == *RepoDigests* ]]; then
      printf '%s' "${ref%@*}@sha256:${key:0:12}"
      exit 0
    fi
    value="FAKE_REV_${key}"
    printf '%s' "${!value:-${FAKE_REV_DEFAULT}}"
    exit 0 ;;
  manifest) shift; log "manifest $*"; exit 0 ;;
esac
exit 0
"""


def publishStepScript():
    lines = workflowPath.read_text().splitlines()
    start = None
    for index, line in enumerate(lines):
        if line == "      - name: Publish":
            for offset in range(index, index + 10):
                if lines[offset] == "        run: |":
                    start = offset + 1
                    break
            break
    if start is None:
        raise AssertionError("the publish workflow has no Publish run block")
    script = []
    for line in lines[start:]:
        if line.strip() and not line.startswith("          "):
            break
        script.append(line[10:])
    return "\n".join(script) + "\n"


class PublishWorkflowTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tmpDir = tempfile.mkdtemp(prefix="publish-workflow-test-")
        binDir = Path(cls.tmpDir) / "bin"
        binDir.mkdir()
        dockerStub = binDir / "docker"
        dockerStub.write_text(DOCKER_STUB)
        dockerStub.chmod(0o755)
        cls.scriptPath = Path(cls.tmpDir) / "publish.sh"
        cls.scriptPath.write_text(publishStepScript())
        cls.logPath = Path(cls.tmpDir) / "docker.log"

    @classmethod
    def tearDownClass(cls):
        shutil.rmtree(cls.tmpDir, ignore_errors=True)

    def publish(self, **environment):
        self.logPath.unlink(missing_ok=True)
        env = dict(os.environ)
        env["PATH"] = f"{Path(self.tmpDir) / 'bin'}:{env['PATH']}"
        env["FAKE_DOCKER_LOG"] = str(self.logPath)
        env["FAKE_REV_DEFAULT"] = "revision-a"
        env.update(environment)
        completed = subprocess.run(
            ["bash", "-e", str(self.scriptPath)],
            cwd=repoRoot,
            env=env,
            stdin=subprocess.DEVNULL,
            capture_output=True,
            text=True,
        )
        log = self.logPath.read_text() if self.logPath.exists() else ""
        return completed, log

    def pushes(self, log):
        return [line for line in log.splitlines() if line.startswith("manifest push")]

    def creates(self, log):
        return [line for line in log.splitlines() if line.startswith("manifest create")]

    def testPublishesBothLegsOfTheSameRevision(self):
        completed, log = self.publish()
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(len(self.pushes(log)), 3)
        for line in self.creates(log):
            self.assertIn("@sha256:", line)

    def testSkipsThePairWithAStaleCounterpart(self):
        x86Ref = dockerRef("kubeovn/kube-ovn", "-x86")
        completed, log = self.publish(**{revisionEnvVar(x86Ref): "revision-old"})
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(len(self.pushes(log)), 2)
        self.assertIn(f"::warning::skipping {dockerRef('kubeovn/kube-ovn')}", completed.stdout)
        self.assertNotIn(f"manifest push {dockerRef('kubeovn/kube-ovn')}\n", log)

    def testSkipsThePairWithoutRevisionLabels(self):
        completed, log = self.publish(FAKE_REV_DEFAULT="")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(self.pushes(log), [])

    def testSkipsThePairWithUnrenderedRevisionLabels(self):
        completed, log = self.publish(FAKE_REV_DEFAULT="<no value>")
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(self.pushes(log), [])

    def testSkipsOnlyThePairThatIsNotPublishedYet(self):
        completed, log = self.publish(FAKE_PULL_MISSING=dockerRef("kubeovn/kube-ovn", "-arm"))
        self.assertEqual(completed.returncode, 0, completed.stderr)
        self.assertEqual(len(self.pushes(log)), 2)
        self.assertIn("is not published yet", completed.stdout)

    def testFailsOnRegistryErrors(self):
        completed, _ = self.publish(FAKE_PULL_ERROR=dockerRef("kubeovn/vpc-nat-gateway", "-x86"))
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("unauthorized", completed.stderr)

    def testFailsOnPullErrorsOtherThanAMissingManifest(self):
        completed, _ = self.publish(
            FAKE_PULL_ERROR=dockerRef("kubeovn/kube-ovn", "-x86"),
            FAKE_PULL_ERROR_MSG="Error response from daemon: blob sha256:deadbeef not found",
        )
        self.assertNotEqual(completed.returncode, 0)

    def testFailsWhenTheImageCannotBeInspected(self):
        completed, _ = self.publish(FAKE_INSPECT_FAIL=dockerRef("kubeovn/kube-ovn", "-arm"))
        self.assertNotEqual(completed.returncode, 0)
        self.assertIn("cannot inspect", completed.stderr)


if __name__ == "__main__":
    unittest.main()
