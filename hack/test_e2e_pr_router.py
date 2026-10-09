#!/usr/bin/env python3

import base64
import copy
import io
import json
import os
import subprocess
import sys
import tempfile
import unittest
import zipfile
from pathlib import Path
from unittest.mock import patch

repoRoot = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(repoRoot / "hack"))

import e2e_pr_router as router


class GitHubStub:
    def __init__(self, responses):
        self.responses = responses
        self.calls = []

    def api(self, path, **kwargs):
        self.calls.append((path, kwargs))
        key = (kwargs.get("method", "GET"), path)
        value = self.responses.get(key, self.responses.get(path))
        if callable(value):
            value = value()
        if isinstance(value, Exception):
            raise value
        return copy.deepcopy(value)


class E2EPRRouterTest(unittest.TestCase):
    repository = "kubeovn/kube-ovn"
    headSHA = "a" * 40
    baseSHA = "b" * 40
    number = 7617

    def setUp(self):
        self.pull = {"number": self.number, "state": "open", "head": {"sha": self.headSHA,
                     "repo": {"full_name": "contributor/kube-ovn"}},
                     "base": {"ref": "release-1.15", "sha": self.baseSHA,
                              "repo": {"full_name": self.repository}}}
        self.pullPath = f"repos/{self.repository}/pulls/{self.number}"
        self.ref = router.candidateWorkflowRef(self.number, self.headSHA)
        self.refPath = f"repos/{self.repository}/git/ref/heads/{self.ref}"
        self.github = GitHubStub({self.pullPath: self.pull, self.refPath: {"object": {"sha": self.headSHA}}})
        self.event = {"repository": {"full_name": self.repository, "default_branch": "master"},
                      "sender": {"login": "maintainer"}, "action": "created",
                      "issue": {"number": self.number, "pull_request": {"url": "https://api.github.com/pull"}},
                      "comment": {"id": 101, "body": "/test e2e core", "user": {"login": "maintainer"}}}
        self.commentPath = f"repos/{self.repository}/issues/comments/101"
        self.github.responses[self.commentPath] = {
            **self.event["comment"], "issue_url": f"https://api.github.com/repos/{self.repository}/issues/{self.number}"}
        self.permissionPath = f"repos/{self.repository}/collaborators/maintainer/permission"
        self.github.responses[self.permissionPath] = {"permission": "write"}

    def prepare(self, eventName="issue_comment", event=None):
        return router.prepare(eventName, event or self.event, self.repository, "maintainer", 88, 1,
                              github=self.github)

    def send(self, **kwargs):
        return router.dispatch(self.repository, self.number, self.headSHA, "x86-e2e-gate.yaml",
                               {"headSHA": self.headSHA}, github=self.github, **kwargs)

    def missing(self, status=404):
        return subprocess.CalledProcessError(1, ["gh"], stderr=f"gh: failure (HTTP {status})".encode())

    def testCandidateRefsValidateIdentity(self):
        self.assertEqual(self.ref, f"x86-e2e/pr-{self.number}-control-{self.headSHA}")
        for number, sha in [(True, self.headSHA), (0, self.headSHA), ("1", self.headSHA),
                            (1, "a" * 39), (1, "A" * 40), (1, "a;echo"), (1, None), (1, 2)]:
            with self.subTest(number=number, sha=sha), self.assertRaises(ValueError):
                router.candidateWorkflowRef(number, sha)

    def testDispatchReusesExactHEADAndAllowsForks(self):
        self.assertEqual(self.send(), self.ref)
        call = self.github.calls[-1]
        self.assertEqual(call[0], f"repos/{self.repository}/actions/workflows/x86-e2e-gate.yaml/dispatches")
        self.assertEqual(call[1]["payload"], {"ref": self.ref, "inputs": {"headSHA": self.headSHA}})
        self.assertEqual(sum(path == self.pullPath for path, _ in self.github.calls), 2)

    def testDispatchCreatesExactHEADRef(self):
        self.github.responses[self.refPath] = self.missing()
        self.send()
        self.assertIn((f"repos/{self.repository}/git/refs", {"method": "POST", "payload": {
            "ref": f"refs/heads/{self.ref}", "sha": self.headSHA}}), self.github.calls)

    def testAuthenticationFailureIsNotMissingRef(self):
        self.github.responses[self.refPath] = self.missing(403)
        with self.assertRaises(subprocess.CalledProcessError):
            self.send()
        self.assertFalse(any(options.get("method") == "POST" for _, options in self.github.calls))

    def testRejectsChangedOrUnexpectedRef(self):
        self.github.responses[self.refPath] = {"object": {"sha": self.baseSHA}}
        with self.assertRaisesRegex(ValueError, "unexpected revision"):
            self.send()
        self.github.responses[self.refPath] = {"object": {"sha": self.headSHA}}
        changed = copy.deepcopy(self.pull)
        changed["head"]["sha"] = self.baseSHA
        pulls = iter([self.pull, changed])
        self.github.responses[self.pullPath] = lambda: next(pulls)
        with self.assertRaisesRegex(ValueError, "HEAD changed"):
            self.send()
        self.assertFalse(any("dispatches" in path for path, _ in self.github.calls))

    def testRefCreationRaceOnlyAcceptsExactHEAD(self):
        for sha in [self.headSHA, self.baseSHA]:
            with self.subTest(sha=sha):
                refs = iter([self.missing(), {"object": {"sha": sha}}])
                self.github.responses[self.refPath] = lambda: next(refs)
                self.github.responses[("POST", f"repos/{self.repository}/git/refs")] = self.missing(422)
                if sha == self.headSHA:
                    self.send()
                else:
                    with self.assertRaises(ValueError):
                        self.send()

    def testDispatchRejectsUnsupportedInputsAndTargets(self):
        for workflow, inputs in [("other.yaml", {}), ("x86-e2e-gate.yaml", {"prNumber": 1})]:
            with self.subTest(workflow=workflow), self.assertRaises(ValueError):
                router.dispatch(self.repository, self.number, self.headSHA, workflow, inputs, github=self.github)
        for field, value in [("state", "closed"), ("number", 1)]:
            with self.subTest(field=field):
                pull = copy.deepcopy(self.pull)
                pull[field] = value
                self.github.responses[self.pullPath] = pull
                with self.assertRaises(ValueError):
                    self.send()
        self.pull["base"]["repo"]["full_name"] = "other/repository"
        self.github.responses[self.pullPath] = self.pull
        with self.assertRaisesRegex(ValueError, "different repository"):
            self.send()

    def testDispatchRejectsMismatchedCandidateInputsBeforeAPICalls(self):
        for changes in [{"headSHA": self.baseSHA}, {"sourceHeadSHA": self.baseSHA},
                        {"sourceHeadSHA": ""}, {"prNumber": "1"}]:
            with self.subTest(changes=changes):
                inputs = {"headSHA": self.headSHA, **changes}
                with self.assertRaisesRegex(ValueError, "identity"):
                    router.dispatch(self.repository, self.number, self.headSHA, "x86-e2e-gate.yaml", inputs, github=self.github)
        self.assertEqual(self.github.calls, [])

    def testDispatchSizeLimitIncludesEscapedNestedSourceEvent(self):
        inputs = {"headSHA": self.headSHA, "sourceHeadSHA": self.headSHA,
                  "sourceEvent": json.dumps({"body": '"' * 20000})}
        self.assertLess(len(inputs["sourceEvent"]), 60000)
        with self.assertRaisesRegex(ValueError, "size limit"):
            router.dispatch(self.repository, self.number, self.headSHA, "x86-e2e-dispatcher.yaml", inputs, github=self.github)
        self.assertEqual(self.github.calls, [])
        inputs["sourceEvent"] = json.dumps({"body": '"' * 100})
        router.dispatch(self.repository, self.number, self.headSHA, "x86-e2e-dispatcher.yaml", inputs, github=self.github)

    def testRoutesAuthenticatedComment(self):
        route = self.prepare()
        self.assertEqual(route["headSHA"], self.headSHA)
        self.assertEqual(route["inputs"]["sourceHeadSHA"], self.headSHA)
        self.assertEqual(route["sourceEvent"]["_router"], {"runId": 88, "runAttempt": 1, "eventName": "issue_comment"})
        self.assertEqual(json.loads(route["inputs"]["sourceEvent"]), route["sourceEvent"])

    def testIgnoresNonCommands(self):
        self.event["comment"]["body"] = "looks good"
        self.assertIsNone(self.prepare())

    def testRejectsUntrustedComment(self):
        for permission in ["read", "triage", "none"]:
            with self.subTest(permission=permission):
                self.github.responses[self.permissionPath] = {"permission": permission}
                with self.assertRaisesRegex(ValueError, "write permission"):
                    self.prepare()
        self.github.responses[self.permissionPath] = {"permission": "write"}
        self.event["comment"]["user"]["login"] = "other"
        with self.assertRaisesRegex(ValueError, "author"):
            self.prepare()

    def testRejectsEditedOrMisplacedComments(self):
        for key, value in [("body", "/test e2e-all"), ("issue_url", "https://api.github.com/repos/other/repo/issues/1")]:
            with self.subTest(key=key):
                live = copy.deepcopy(self.github.responses[self.commentPath])
                live[key] = value
                self.github.responses[self.commentPath] = live
                with self.assertRaisesRegex(ValueError, "changed"):
                    self.prepare()

    def testRejectsSpoofedActorAndRepository(self):
        self.event["sender"]["login"] = "other"
        with self.assertRaisesRegex(ValueError, "actor"):
            self.prepare()
        self.event["repository"]["full_name"] = "other/repo"
        with self.assertRaisesRegex(ValueError, "repository"):
            self.prepare()

    def testClosedPullRequestCanRouteCancellation(self):
        self.pull["state"] = "closed"
        self.github.responses[self.pullPath] = self.pull
        event = {**self.event, "action": "closed", "pull_request": self.pull}
        route = self.prepare("pull_request_target", event)
        self.assertTrue(route["allowClosed"])
        self.send(allowClosed=True)
        with self.assertRaises(ValueError):
            self.send()

    def testPushRoutesEachCandidateWithBaseBinding(self):
        event = {**self.event, "ref": "refs/heads/release-1.15", "after": self.baseSHA}
        self.github.responses[f"repos/{self.repository}/git/ref/heads/release-1.15"] = {"object": {"sha": self.baseSHA}}
        self.github.responses[f"repos/{self.repository}/pulls?state=open&base=release-1.15&per_page=100&page=1"] = [self.pull]
        routes = self.prepare("push", event)
        self.assertEqual(len(routes), 1)
        self.assertEqual(routes[0]["inputs"]["headSHA"], self.headSHA)
        self.assertEqual(routes[0]["sourceEvent"]["after"], self.baseSHA)
        event["after"] = "c" * 40
        self.assertEqual(self.prepare("push", event), [])

    def executor(self):
        metadata = {"prNumber": self.number, "headSHA": self.headSHA, "approvalGeneration": 2,
                    "dispatchGeneration": 3, "requestedGroups": ["core"], "controlledLabels": [], "full": False}
        run = {"id": 99, "path": ".github/workflows/build-x86-image.yaml", "event": "workflow_dispatch",
               "actor": {"login": "github-actions[bot]"}, "status": "completed", "run_attempt": 1,
               "head_sha": self.headSHA, "head_branch": router.e2eControl.executorHeadBranch(metadata),
               "display_title": f"x86-e2e pr={self.number} head={self.headSHA} approval=2 generation=3 groups=core labels=- full=0 base={self.baseSHA}"}
        event = {**self.event, "action": "completed", "workflow_run": {"id": 99, "run_attempt": 1}}
        self.github.responses[f"repos/{self.repository}/actions/runs/99"] = run
        self.github.responses[f"repos/{self.repository}/git/ref/heads/release-1.15"] = {"object": {"sha": self.baseSHA}}
        catalog = (repoRoot / ".github/e2e-selection.json").read_bytes()
        self.github.responses[f"repos/{self.repository}/contents/.github/e2e-selection.json?ref={self.headSHA}"] = {
            "content": base64.b64encode(catalog).decode()}
        return run, event

    def testCompletedCandidateExecutorRoutesCandidateGate(self):
        _, event = self.executor()
        route = self.prepare("workflow_run", event)
        self.assertEqual(route["workflow"], "x86-e2e-gate.yaml")
        self.assertEqual(route["inputs"]["executorRunId"], "99")
        self.assertEqual(route["inputs"]["baseSHA"], self.baseSHA)

    def testCompletedExecutorLeavesCatalogValidationToCandidateGate(self):
        _, event = self.executor()
        catalogPath = f"repos/{self.repository}/contents/.github/e2e-selection.json?ref={self.headSHA}"
        self.github.responses[catalogPath] = {
            "content": base64.b64encode(json.dumps({"schemaVersion": 2}).encode()).decode()}
        route = self.prepare("workflow_run", event)
        self.assertNotIn("catalogRevision", route["inputs"])
        self.assertFalse(any(path == catalogPath for path, _ in self.github.calls))

    def testCompletedExecutorPreservesActualBaseAfterBaseAdvance(self):
        _, event = self.executor()
        self.github.responses[f"repos/{self.repository}/git/ref/heads/release-1.15"] = {
            "object": {"sha": "c" * 40}}
        route = self.prepare("workflow_run", event)
        self.assertEqual(route["inputs"]["baseSHA"], self.baseSHA)

    def testCompletedExecutorWithoutBaseIdentityIsRejected(self):
        run, event = self.executor()
        run["display_title"] = run["display_title"].split(" base=")[0]
        with self.assertRaisesRegex(ValueError, "base identity"):
            self.prepare("workflow_run", event)

    def testRejectsOldBaseOrSpoofedExecutor(self):
        for key, value in [("head_sha", self.baseSHA), ("head_branch", "master"), ("run_attempt", 2),
                           ("actor", {"login": "maintainer"}), ("status", "in_progress"),
                           ("path", ".github/workflows/other.yaml")]:
            with self.subTest(key=key):
                run, event = self.executor()
                run[key] = value
                with self.assertRaises(ValueError):
                    self.prepare("workflow_run", event)

    def testOrdinaryImageBuildsDoNotRouteToPRGate(self):
        for eventName in ["push", "workflow_dispatch"]:
            run, event = self.executor()
            run.update({"event": eventName, "display_title": "Build release image"})
            self.assertIsNone(self.prepare("workflow_run", event))

    def authenticatedSource(self):
        source = self.prepare()["sourceEvent"]
        self.github.responses[f"repos/{self.repository}/actions/runs/88"] = {
            "path": router.ROUTER_PATH, "event": "issue_comment", "actor": {"login": "maintainer"}, "run_attempt": 1}
        self.github.responses[f"repos/{self.repository}/actions/runs/88/artifacts?per_page=100"] = {
            "artifacts": [{"id": 5, "name": "x86-e2e-router-event-1", "expired": False}]}
        self.setArtifact([source])
        return source

    def setArtifact(self, sources):
        buffer = io.BytesIO()
        with zipfile.ZipFile(buffer, "w") as archive:
            archive.writestr("source-events.json", json.dumps(sources))
        self.github.responses[f"repos/{self.repository}/actions/artifacts/5/zip"] = buffer.getvalue()

    def validate(self, source, sourceName="issue_comment", eventName="workflow_dispatch"):
        with patch.dict(os.environ, {"GITHUB_ACTOR": "github-actions[bot]"}):
            return router.validateSource(eventName, {}, sourceName, source, self.number, self.headSHA,
                                         self.repository, github=self.github)

    def testCanonicalSourceAuthenticates(self):
        source = self.authenticatedSource()
        self.assertEqual(self.validate(source), source)

    def testRejectsTamperedOrAmbiguousCanonicalEvent(self):
        source = self.authenticatedSource()
        changed = copy.deepcopy(source)
        changed["comment"]["body"] = "/test e2e-all"
        with self.assertRaisesRegex(ValueError, "canonical"):
            self.validate(changed)
        self.setArtifact([source, source])
        with self.assertRaisesRegex(ValueError, "canonical"):
            self.validate(source)

    def testRejectsWrongRouterRunAndMissingArtifact(self):
        source = self.authenticatedSource()
        self.github.responses[f"repos/{self.repository}/actions/runs/88"]["path"] = ".github/workflows/other.yaml"
        with self.assertRaisesRegex(ValueError, "router run"):
            self.validate(source)
        source = self.authenticatedSource()
        self.github.responses[f"repos/{self.repository}/actions/runs/88/artifacts?per_page=100"]["artifacts"] = []
        with self.assertRaisesRegex(ValueError, "artifact"):
            self.validate(source)

    def testRawDispatchRequiresBotAndCurrentHEAD(self):
        with patch.dict(os.environ, {"GITHUB_ACTOR": "maintainer"}), self.assertRaises(ValueError):
            router.validateSource("workflow_dispatch", {}, "", {}, self.number, self.headSHA,
                                  self.repository, github=self.github)
        self.assertEqual(self.validate({}, ""), {})
        with self.assertRaises(ValueError):
            self.validate({}, "", "push")

    def testPushSourceRequiresCurrentBaseAndExactCandidate(self):
        source = self.authenticatedSource()
        source.update({"ref": "refs/heads/release-1.15", "after": self.baseSHA,
                       "pull_request": {"number": self.number, "head": {"sha": self.headSHA}}})
        source["_router"]["eventName"] = "push"
        self.github.responses[f"repos/{self.repository}/actions/runs/88"]["event"] = "push"
        self.github.responses[f"repos/{self.repository}/git/ref/heads/release-1.15"] = {"object": {"sha": self.baseSHA}}
        self.setArtifact([source])
        self.assertEqual(self.validate(source, "push"), source)
        self.github.responses[f"repos/{self.repository}/git/ref/heads/release-1.15"]["object"]["sha"] = "c" * 40
        with self.assertRaisesRegex(ValueError, "stale"):
            self.validate(source, "push")

    def testRouterArchivesBeforeSequentialCandidateDispatch(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-pr-router.yaml").read_text()
        self.assertIn("  workflow_run:", workflow)
        self.assertIn("  issue_comment:", workflow)
        self.assertIn("  pull_request_target:", workflow)
        self.assertIn("branches: ['release-*']", workflow)
        self.assertLess(workflow.index("actions/upload-artifact"), workflow.index("dispatch-prepared"))

    def testPrepareAndDispatchCLIKeepCanonicalJSONAndStringInputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            eventPath = directory / "event.json"
            eventPath.write_text(json.dumps(self.event))
            outputPath = directory / "output"
            eventDirectory = directory / "router"
            environment = {"GITHUB_REPOSITORY": self.repository, "GITHUB_ACTOR": "maintainer",
                           "GITHUB_RUN_ID": "88", "GITHUB_RUN_ATTEMPT": "1", "GITHUB_OUTPUT": str(outputPath)}
            argv = ["router", "prepare", "--event-file", str(eventPath), "--event-name", "issue_comment",
                    "--output-dir", str(eventDirectory)]
            with patch.dict(os.environ, environment), patch.object(sys, "argv", argv), patch.object(router, "GitHub", return_value=self.github):
                router.main()
            routes = json.loads((eventDirectory / "routes.json").read_text())
            sources = json.loads((eventDirectory / "source-events.json").read_text())
            self.assertEqual(json.loads(routes[0]["inputs"]["sourceEvent"]), sources[0])
            self.assertEqual(outputPath.read_text(), "route=true\n")
            argv = ["router", "dispatch-prepared", "--routes-file", str(eventDirectory / "routes.json")]
            with patch.dict(os.environ, environment), patch.object(sys, "argv", argv), patch.object(router, "dispatch") as send, patch("builtins.print"):
                router.main()
            self.assertEqual(send.call_args.args[:4], (self.repository, self.number, self.headSHA, "x86-e2e-dispatcher.yaml"))
            self.assertTrue(all(isinstance(value, str) for value in send.call_args.args[4].values()))

    def testSourceValidationCLIRejectsBaseWorkflowRevision(self):
        environment = {"GITHUB_REPOSITORY": self.repository, "GITHUB_SHA": self.baseSHA, "CANDIDATE_HEAD": self.headSHA}
        with patch.dict(os.environ, environment), patch.object(sys, "argv", ["router", "validate-source", "--output-file", "/unused"]):
            with self.assertRaisesRegex(ValueError, "workflow did not execute"):
                router.main()

    def testDispatcherUsesCandidatesAndAuthenticatedDependencies(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-dispatcher.yaml").read_text()
        self.assertNotIn("  issue_comment:", workflow)
        self.assertNotIn("  pull_request_target:", workflow)
        self.assertNotIn("  push:", workflow)
        self.assertEqual(workflow.count("always() && needs.validate-source.result == 'success'"), 2)
        self.assertIn('printf \'%s\\t%s\\n\' "$PR_NUMBER" "$CANDIDATE_HEAD"', workflow)
        self.assertNotIn('sha="$baseSHA"', workflow)
        self.assertNotIn('ref: ${{ github.event.repository.default_branch }}', workflow)


if __name__ == "__main__":
    unittest.main()
