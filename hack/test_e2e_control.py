#!/usr/bin/env python3

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

repoRoot = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(repoRoot / "hack"))

import e2e_control as e2eControl
import e2e_selector as e2eSelector


class E2EControlTest(unittest.TestCase):
    def testParsesSupportedCommentCommands(self):
        cases = {
            "/test e2e": ("dispatch", [], False),
            "/test e2e policy,multi-cni": ("dispatch", ["multi-cni", "policy"], False),
            "/test e2e-all": ("dispatch", [], True),
            "/retest e2e-failed": ("rerun-failed", [], False),
        }

        for body, expected in cases.items():
            with self.subTest(body=body):
                command = e2eControl.parseCommand(body)
                self.assertEqual(
                    (command["action"], command["requestedGroups"], command["full"]),
                    expected,
                )

    def testRejectsMalformedOrInjectedCommands(self):
        for body in [
            "/test e2e policy bogus",
            "/test e2e policy;echo-owned",
            "/test e2e policy\n/test e2e-all",
            "/test e2e policy,",
            "/test e2e Policy",
        ]:
            with self.subTest(body=body):
                with self.assertRaisesRegex(ValueError, "invalid E2E command"):
                    e2eControl.parseCommand(body)

    def testIgnoresUnrelatedComments(self):
        self.assertIsNone(e2eControl.parseCommand("please test this change"))

    def dispatchDecision(
        self,
        body="/test e2e policy",
        permission="write",
        observedHeadSHA="a" * 40,
        confirmedHeadSHA="a" * 40,
        state="open",
        baseRef="master",
    ):
        event = {
            "action": "created",
            "comment": {"body": body, "user": {"login": "maintainer"}},
            "issue": {"number": 7231, "pull_request": {"url": "https://api.example/pr/7231"}},
            "repository": {"full_name": "kubeovn/kube-ovn"},
        }
        pullRequest = {
            "number": 7231,
            "state": state,
            "head": {"sha": observedHeadSHA},
            "base": {"ref": baseRef},
        }
        catalog = json.loads((repoRoot / ".github/e2e-selection.json").read_text())
        return e2eControl.decideDispatch(
            event,
            permission,
            pullRequest,
            confirmedHeadSHA,
            set(catalog["groups"]),
            e2eSelector.catalogRevision(catalog),
        )

    def testAuthorizedCommandIsBoundToCurrentHead(self):
        decision = self.dispatchDecision()

        self.assertTrue(decision["accepted"])
        self.assertEqual(decision["action"], "dispatch")
        self.assertEqual(decision["prNumber"], 7231)
        self.assertEqual(decision["headSHA"], "a" * 40)
        self.assertEqual(decision["baseRef"], "master")
        self.assertEqual(decision["requestedGroups"], ["policy"])
        self.assertRegex(decision["requestKey"], r"^[0-9a-f]{64}$")

    def testOnlyWritePermissionCanDispatch(self):
        for permission in ["admin", "maintain", "write"]:
            with self.subTest(permission=permission):
                self.assertTrue(self.dispatchDecision(permission=permission)["accepted"])
        for permission in ["triage", "read", "none"]:
            with self.subTest(permission=permission):
                decision = self.dispatchDecision(permission=permission)
                self.assertFalse(decision["accepted"])
                self.assertEqual(decision["reason"], "repository write permission is required")

    def testStaleHeadIsRejectedImmediatelyBeforeDispatch(self):
        decision = self.dispatchDecision(confirmedHeadSHA="b" * 40)

        self.assertFalse(decision["accepted"])
        self.assertEqual(decision["reason"], "pull request HEAD changed; comment again")

    def testClosedPullRequestIsRejected(self):
        decision = self.dispatchDecision(state="closed")

        self.assertFalse(decision["accepted"])
        self.assertEqual(decision["reason"], "pull request is not open")

    def testUnsupportedBaseBranchIsRejected(self):
        decision = self.dispatchDecision(baseRef="feature/test")

        self.assertFalse(decision["accepted"])
        self.assertEqual(decision["reason"], "pull request base branch is not supported")

    def testUnknownRequestedGroupIsRejectedBeforeDispatch(self):
        decision = self.dispatchDecision(body="/test e2e not-a-group")

        self.assertFalse(decision["accepted"])
        self.assertEqual(decision["reason"], "unknown requested E2E group: not-a-group")

    def testUnrelatedCommentProducesNoDecision(self):
        self.assertIsNone(self.dispatchDecision(body="looks good"))

    def testInitialGateWaitsForRequiredComment(self):
        plan = {"headSHA": "a" * 40, "approvalRequired": True}

        decision = e2eControl.evaluateGate(
            plan,
            "a" * 40,
            "success",
            approved=False,
            executedPlan=plan,
        )

        self.assertTrue(decision["update"])
        self.assertEqual(decision["conclusion"], "action_required")
        self.assertIn("/test e2e", decision["summary"])

    def testApprovedExecutorCanSatisfyGate(self):
        plan = {"headSHA": "a" * 40, "approvalRequired": True}

        decision = e2eControl.evaluateGate(
            plan,
            "a" * 40,
            "success",
            approved=True,
            executedPlan=plan,
        )

        self.assertEqual(decision["conclusion"], "success")

    def testChangedSelectionPlanRequiresNewExecution(self):
        plan = {"headSHA": "a" * 40, "approvalRequired": False, "selectedGroups": ["policy"]}
        executedPlan = {"headSHA": "a" * 40, "approvalRequired": False, "selectedGroups": []}

        decision = e2eControl.evaluateGate(
            plan,
            "a" * 40,
            "success",
            approved=True,
            executedPlan=executedPlan,
        )

        self.assertEqual(decision["conclusion"], "action_required")
        self.assertIn("selection plan changed", decision["summary"])

    def testIncompatibleCatalogRevisionFailsGate(self):
        plan = {
            "headSHA": "a" * 40,
            "approvalRequired": False,
            "catalogRevision": "b" * 64,
        }

        decision = e2eControl.evaluateGate(
            plan,
            "a" * 40,
            "success",
            approved=True,
            expectedCatalogRevision="c" * 64,
        )

        self.assertEqual(decision["conclusion"], "failure")
        self.assertIn("catalog revision", decision["summary"])

    def testFailedExecutorFailsGate(self):
        plan = {"headSHA": "a" * 40, "approvalRequired": False}

        decision = e2eControl.evaluateGate(plan, "a" * 40, "failure", approved=True)

        self.assertEqual(decision["conclusion"], "failure")

    def testStaleRunCannotUpdateNewHeadGate(self):
        plan = {"headSHA": "a" * 40, "approvalRequired": False}

        decision = e2eControl.evaluateGate(plan, "b" * 40, "success", approved=True)

        self.assertFalse(decision["update"])
        self.assertEqual(decision["reason"], "workflow result belongs to a stale pull request HEAD")

    def testApprovedRequestsAccumulateOnlyOnTheSameHead(self):
        decision = self.dispatchDecision(body="/test e2e policy")
        merged = e2eControl.mergeApprovedRequests(
            decision,
            [
                {"headSHA": "a" * 40, "requestedGroups": ["multi-cni"], "full": False},
                {"headSHA": "b" * 40, "requestedGroups": ["nat-egress"], "full": True},
            ],
        )

        self.assertEqual(merged["requestedGroups"], ["multi-cni", "policy"])
        self.assertFalse(merged["full"])
        self.assertNotEqual(merged["requestKey"], decision["requestKey"])

    def testFullApprovalPersistsForTheCurrentHead(self):
        decision = self.dispatchDecision(body="/test e2e policy")
        merged = e2eControl.mergeApprovedRequests(
            decision,
            [{"headSHA": "a" * 40, "requestedGroups": [], "full": True}],
        )

        self.assertTrue(merged["full"])

    def testRequestMarkerRoundTripsWithoutActiveMarkdown(self):
        request = {
            "headSHA": "a" * 40,
            "requestedGroups": ["multi-cni", "policy"],
            "full": False,
        }
        marker = e2eControl.renderRequestMarker(request)

        self.assertEqual(e2eControl.parseRequestMarker(marker), request)
        self.assertTrue(marker.startswith("<!-- x86-e2e-request "))

    def testParsesTrustedExecutorRunName(self):
        request = {
            "prNumber": 7231,
            "headSHA": "a" * 40,
            "catalogRevision": "c" * 64,
            "requestedGroups": ["multi-cni", "policy"],
            "full": False,
        }
        requestKey = e2eControl.executorRequestKey(request)
        metadata = e2eControl.parseExecutorRunName(
            "x86-e2e pr=7231 head="
            + "a" * 40
            + " request="
            + requestKey
            + " catalog="
            + "c" * 64
            + " groups=multi-cni,policy full=0"
        )

        self.assertEqual(metadata["prNumber"], 7231)
        self.assertEqual(metadata["headSHA"], "a" * 40)
        self.assertEqual(metadata["requestKey"], requestKey)
        self.assertEqual(metadata["catalogRevision"], "c" * 64)
        self.assertEqual(metadata["requestedGroups"], ["multi-cni", "policy"])
        self.assertFalse(metadata["full"])

    def testRejectsExecutorRunNameWithTamperedRequestKey(self):
        with self.assertRaisesRegex(ValueError, "request key"):
            e2eControl.parseExecutorRunName(
                "x86-e2e pr=7231 head="
                + "a" * 40
                + " request="
                + "b" * 64
                + " catalog="
                + "c" * 64
                + " groups=multi-cni,policy full=0"
            )

    def testLatestExecutorRunRejectsUntrustedCandidates(self):
        request = {
            "prNumber": 7231,
            "headSHA": "a" * 40,
            "catalogRevision": "c" * 64,
            "requestedGroups": ["policy"],
            "full": False,
        }
        requestKey = e2eControl.executorRequestKey(request)
        title = (
            "x86-e2e pr=7231 head="
            + "a" * 40
            + " request="
            + requestKey
            + " catalog="
            + "c" * 64
            + " groups=policy full=0"
        )
        trusted = {
            "id": 3,
            "path": ".github/workflows/build-x86-image.yaml",
            "actor": {"login": "github-actions[bot]"},
            "head_branch": "master",
            "display_title": title,
        }
        runs = [
            {**trusted, "id": 1, "path": ".github/workflows/other.yaml"},
            {**trusted, "id": 2, "actor": {"login": "maintainer"}},
            trusted,
        ]

        latest = e2eControl.latestExecutorRun(runs, 7231, "a" * 40, "master")

        self.assertEqual(latest["id"], 3)

    def testDispatchCliWritesDecisionJson(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            event = directory / "event.json"
            pullRequest = directory / "pr.json"
            decision = directory / "decision.json"
            event.write_text(
                json.dumps(
                    {
                        "action": "created",
                        "comment": {"body": "/test e2e policy", "user": {"login": "maintainer"}},
                        "issue": {"number": 7231, "pull_request": {"url": "https://api.example/pr/7231"}},
                        "repository": {"full_name": "kubeovn/kube-ovn"},
                    }
                )
            )
            pullRequest.write_text(
                json.dumps(
                    {
                        "number": 7231,
                        "state": "open",
                        "head": {"sha": "a" * 40},
                        "base": {"ref": "master"},
                    }
                )
            )

            subprocess.run(
                [
                    sys.executable,
                    str(repoRoot / "hack/e2e_control.py"),
                    "dispatch",
                    "--event-file",
                    str(event),
                    "--permission",
                    "write",
                    "--pull-request-file",
                    str(pullRequest),
                    "--confirmed-head-sha",
                    "a" * 40,
                    "--decision-file",
                    str(decision),
                ],
                cwd=repoRoot,
                check=True,
            )

            self.assertTrue(json.loads(decision.read_text())["accepted"])

    def testDispatcherWorkflowUsesOnlyTrustedWritePermissions(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-dispatcher.yaml").read_text()

        self.assertIn("issue_comment:\n    types: [created]", workflow)
        self.assertIn("actions: write", workflow)
        self.assertIn("pull-requests: read", workflow)
        self.assertIn("issues: write", workflow)
        self.assertIn("--catalog trusted-catalog.json", workflow)
        self.assertIn("e2e-selection.json?ref=$baseRef", workflow)
        self.assertNotIn("contents: write", workflow)
        self.assertNotIn("pull_request_target", workflow)
        self.assertNotIn("inputs.headSHA || github.sha", workflow)

    def testGateWorkflowCanOnlyReadRunsAndWriteChecks(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-gate.yaml").read_text()

        self.assertIn("workflow_run:", workflow)
        self.assertIn("actions: read", workflow)
        self.assertIn("checks: write", workflow)
        self.assertIn("pull-requests: read", workflow)
        self.assertIn("RUN_PATH: ${{ github.event.workflow_run.path }}", workflow)
        self.assertIn("RUN_ACTOR: ${{ github.event.workflow_run.actor.login }}", workflow)
        self.assertIn("RUN_ATTEMPT: ${{ github.event.workflow_run.run_attempt }}", workflow)
        self.assertIn("group: x86-e2e-required-gate-mutations", workflow)
        self.assertIn("cancel-in-progress: false", workflow)
        self.assertIn("ignore the delayed requested event", workflow)
        self.assertIn("Download the executed SelectionPlan", workflow)
        self.assertIn("executed-selection-plan.json", workflow)
        self.assertIn("ref: ${{ github.event.repository.default_branch }}", workflow)
        self.assertIn("ref: ${{ steps.context.outputs.trustedRef }}", workflow)
        self.assertNotIn("ref: ${{ steps.context.outputs.baseRef }}", workflow)
        self.assertNotIn("inputs.headSHA || github.sha", workflow)
        self.assertNotIn("contents: write", workflow)

    def testExecutorDispatchKeepsPullRequestAndPushCoverageUnchanged(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        blocks = e2eSelector.workflowJobBlocks(workflow)
        catalog = e2eSelector.loadCatalog(repoRoot / ".github/e2e-selection.json")
        testJobs = {
            job["id"]
            for group in catalog["groups"].values()
            for job in group["jobs"]
        }

        self.assertIn("workflow_dispatch:", workflow)
        self.assertIn("unknown requested E2E group", workflow)
        self.assertIn("executor catalog revision does not match dispatcher", workflow)
        self.assertNotIn('entry["selection"] != "smoke"', workflow)
        self.assertIn("contents: read", workflow)
        self.assertIn("packages: read", workflow)
        self.assertNotIn("actions: write", workflow)
        self.assertNotIn("checks: write", workflow)
        self.assertNotIn("GHCR_TOKEN: ${{ secrets.GITHUB_TOKEN }}", workflow)
        self.assertIn(
            "GHCR_TOKEN: ${{ github.event_name != 'workflow_dispatch' && secrets.GITHUB_TOKEN || '' }}",
            workflow,
        )
        resultBlock = blocks["e2e-executor-result"]
        self.assertIn("permissions: {}", resultBlock)
        self.assertIn("selected x86 E2E Jobs did not succeed", resultBlock)
        for jobId in testJobs:
            with self.subTest(jobId=jobId):
                self.assertIn(f"      - {jobId}\n", resultBlock)
                block = blocks[jobId]
                self.assertRegex(block, r"(?m)^    needs:(?:\n      - .+)+\n")
                self.assertIn("e2e-selection", block)
                self.assertIn("github.event_name != 'workflow_dispatch'", block)
                self.assertIn("inputs.headSHA || github.sha", block)
                self.assertIn(
                    "persist-credentials: ${{ github.event_name != 'workflow_dispatch' }}",
                    block,
                )
        self.assertIn("github.event_name != 'workflow_dispatch'", blocks["push"])

    def testKindPullUsesAnonymousGhcrWhenTokenIsAbsent(self):
        makefile = (repoRoot / "makefiles/kind.mk").read_text()

        self.assertIn('if [ -n "$${GHCR_TOKEN:-}" ]', makefile)
        self.assertNotIn(
            "@echo $${GHCR_TOKEN} | docker login ghcr.io",
            makefile,
        )


if __name__ == "__main__":
    unittest.main()
