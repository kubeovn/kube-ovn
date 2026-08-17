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
            "comment": {"id": 1001, "body": body, "user": {"login": "maintainer"}},
            "issue": {"number": 7231, "pull_request": {"url": "https://api.example/pr/7231"}},
            "repository": {"full_name": "kubeovn/kube-ovn"},
        }
        pullRequest = {
            "number": 7231,
            "state": state,
            "head": {"sha": observedHeadSHA},
            "base": {"ref": baseRef, "sha": "b" * 40},
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
        self.assertEqual(decision["baseSHA"], "b" * 40)
        self.assertEqual(decision["approvalGeneration"], 1001)
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

    def testPullRequestWorkflowCannotSatisfyTheFixedGate(self):
        plan = {"headSHA": "a" * 40, "approvalRequired": False}

        decision = e2eControl.evaluateGate(
            plan,
            "a" * 40,
            "success",
            approved=False,
            executedPlan=plan,
            trustedExecution=False,
        )

        self.assertEqual(decision["conclusion"], "action_required")
        self.assertIn("trusted executor", decision["summary"])

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
                {**decision, "requestedGroups": ["multi-cni"], "full": False},
                {**decision, "headSHA": "b" * 40, "requestedGroups": ["nat-egress"], "full": True},
                {**decision, "baseSHA": "c" * 40, "requestedGroups": ["nat-egress"], "full": True},
                {**decision, "catalogRevision": "d" * 64, "requestedGroups": ["nat-egress"], "full": True},
            ],
        )

        self.assertEqual(merged["requestedGroups"], ["multi-cni", "policy"])
        self.assertFalse(merged["full"])
        self.assertNotEqual(merged["requestKey"], decision["requestKey"])

    def testFullApprovalPersistsForTheCurrentHead(self):
        decision = self.dispatchDecision(body="/test e2e policy")
        merged = e2eControl.mergeApprovedRequests(
            decision,
            [{**decision, "requestedGroups": [], "full": True}],
        )

        self.assertTrue(merged["full"])

    def testRequestMarkerRoundTripsWithoutActiveMarkdown(self):
        request = {
            "headSHA": "a" * 40,
            "baseSHA": "b" * 40,
            "approvalGeneration": 1001,
            "catalogRevision": "c" * 64,
            "requestedGroups": ["multi-cni", "policy"],
            "full": False,
        }
        marker = e2eControl.renderRequestMarker(request)

        self.assertEqual(e2eControl.parseRequestMarker(marker), request)
        self.assertTrue(marker.startswith("<!-- x86-e2e-request "))

    def testApprovalIntentsAreTrustedAndCumulative(self):
        catalog = e2eSelector.loadCatalog(repoRoot / ".github/e2e-selection.json")
        intent = {
            "headSHA": "a" * 40,
            "baseSHA": "b" * 40,
            "approvalGeneration": 1001,
            "catalogRevision": e2eSelector.catalogRevision(catalog),
            "requestedGroups": ["policy"],
            "full": False,
        }
        marker = e2eControl.renderApprovalIntent(intent)
        pullRequest = {
            "number": 7231,
            "head": {"sha": "a" * 40},
            "base": {"ref": "master", "sha": "b" * 40},
        }
        pages = [[
            {"user": {"login": "github-actions[bot]", "type": "Bot"}, "body": marker},
            {"user": {"login": "attacker", "type": "User"}, "body": marker},
        ]]

        intents = e2eControl.approvalIntents(pullRequest, catalog, pages)

        self.assertEqual(intents, [intent])

    def testRerunAuthorizationMarkerBindsExactAttempt(self):
        marker = e2eControl.renderRerunMarker(42, 2, "a" * 40, "b" * 40)
        pages = [[
            {"user": {"login": "github-actions[bot]", "type": "Bot"}, "body": marker},
        ]]

        self.assertTrue(e2eControl.hasAuthorizedRerun(pages, 42, 2, "a" * 40, "b" * 40))
        self.assertFalse(e2eControl.hasAuthorizedRerun(pages, 42, 3, "a" * 40, "b" * 40))
        self.assertFalse(
            e2eControl.hasAuthorizedRerun(
                [[{"user": {"login": "attacker", "type": "User"}, "body": marker}]],
                42,
                2,
                "a" * 40,
                "b" * 40,
            )
        )
        self.assertTrue(
            e2eControl.isAuthorizedRunAttempt(
                {"id": 42, "run_attempt": 1}, [], "a" * 40, "b" * 40
            )
        )
        self.assertTrue(
            e2eControl.isAuthorizedRunAttempt(
                {
                    "id": 42,
                    "run_attempt": 2,
                    "triggering_actor": {"login": "github-actions[bot]"},
                },
                pages,
                "a" * 40,
                "b" * 40,
            )
        )
        self.assertFalse(
            e2eControl.isAuthorizedRunAttempt(
                {
                    "id": 42,
                    "run_attempt": 2,
                    "triggering_actor": {"login": "maintainer"},
                },
                pages,
                "a" * 40,
                "b" * 40,
            )
        )
        self.assertFalse(
            e2eControl.isAuthorizedRunAttempt(
                {
                    "id": 42,
                    "run_attempt": 3,
                    "triggering_actor": {"login": "github-actions[bot]"},
                },
                pages,
                "a" * 40,
                "b" * 40,
            )
        )

    def testApprovedRequestUsesTrustedMarkersAndLatestGeneration(self):
        catalog = json.loads((repoRoot / ".github/e2e-selection.json").read_text())
        pullRequest = {
            "number": 7231,
            "head": {"sha": "a" * 40},
            "base": {"ref": "master", "sha": "b" * 40},
        }
        first = self.dispatchDecision(body="/test e2e policy")
        second = {**first, "approvalGeneration": 1002, "requestedGroups": ["multi-cni"]}
        pages = [[
            {"user": {"login": "attacker", "type": "User"}, "body": e2eControl.renderRequestMarker(second)},
            {"user": {"login": "github-actions[bot]", "type": "Bot"}, "body": e2eControl.renderRequestMarker(first)},
            {"user": {"login": "github-actions[bot]", "type": "Bot"}, "body": e2eControl.renderRequestMarker(second)},
        ]]

        request = e2eControl.approvedRequest(pullRequest, catalog, pages)

        self.assertEqual(request["approvalGeneration"], 1002)
        self.assertEqual(request["requestedGroups"], ["multi-cni", "policy"])
        self.assertEqual(request["baseSHA"], "b" * 40)

    def testParsesTrustedExecutorRunName(self):
        request = {
            "prNumber": 7231,
            "headSHA": "a" * 40,
            "baseSHA": "d" * 40,
            "approvalGeneration": 1001,
            "dispatchGeneration": 2001,
            "catalogRevision": "c" * 64,
            "requestedGroups": ["multi-cni", "policy"],
            "full": False,
        }
        metadata = e2eControl.parseExecutorRunName(
            "x86-e2e pr=7231 head="
            + "a" * 40
            + " approval=1001 generation=2001 groups=multi-cni,policy full=0"
        )

        self.assertEqual(metadata["prNumber"], 7231)
        self.assertEqual(metadata["headSHA"], "a" * 40)
        self.assertEqual(metadata["approvalGeneration"], 1001)
        self.assertEqual(metadata["dispatchGeneration"], 2001)
        self.assertEqual(metadata["requestedGroups"], ["multi-cni", "policy"])
        self.assertFalse(metadata["full"])

    def testFullRequestIdentityIgnoresRequestedGroups(self):
        request = {
            "prNumber": 7231,
            "headSHA": "a" * 40,
            "baseSHA": "d" * 40,
            "catalogRevision": "c" * 64,
            "requestedGroups": [],
            "full": True,
        }

        withGroups = {**request, "requestedGroups": ["policy"]}
        merged = e2eControl.mergeApprovedRequests(
            request,
            [{**withGroups, "approvalGeneration": 1001}],
        )

        self.assertEqual(e2eControl.executorRequestKey(request), e2eControl.executorRequestKey(withGroups))
        self.assertEqual(merged["requestedGroups"], [])

    def testExecutorRequestKeyBindsBaseRevision(self):
        request = {
            "prNumber": 7231,
            "headSHA": "a" * 40,
            "baseSHA": "d" * 40,
            "catalogRevision": "c" * 64,
            "requestedGroups": ["multi-cni", "policy"],
            "full": False,
        }

        first = e2eControl.executorRequestKey(request)
        second = e2eControl.executorRequestKey({**request, "baseSHA": "e" * 40})

        self.assertNotEqual(first, second)

    def testRejectsMalformedExecutorRunName(self):
        with self.assertRaisesRegex(ValueError, "invalid x86 E2E executor run name"):
            e2eControl.parseExecutorRunName(
                "x86-e2e pr=7231 head="
                + "a" * 40
                + " approval=0 generation=2001 groups=multi-cni,policy full=0"
            )

    def testLatestExecutorRunRejectsUntrustedCandidates(self):
        request = {
            "prNumber": 7231,
            "headSHA": "a" * 40,
            "baseSHA": "d" * 40,
            "approvalGeneration": 1001,
            "dispatchGeneration": 2001,
            "catalogRevision": "c" * 64,
            "requestedGroups": ["policy"],
            "full": False,
        }
        self.assertEqual(
            e2eControl.executorHeadBranch(request),
            "x86-e2e/pr-7231-a-1001-d-2001",
        )
        title = (
            "x86-e2e pr=7231 head="
            + "a" * 40
            + " approval=1001 generation=2001 groups=policy full=0"
        )
        trusted = {
            "id": 3,
            "path": ".github/workflows/build-x86-image.yaml",
            "actor": {"login": "github-actions[bot]"},
            "head_branch": "x86-e2e/pr-7231-a-1001-d-2001",
            "head_sha": "d" * 40,
            "display_title": title,
        }
        runs = [
            {**trusted, "id": 1, "path": ".github/workflows/other.yaml"},
            {**trusted, "id": 2, "actor": {"login": "maintainer"}},
            {**trusted, "id": 4, "head_branch": "master"},
            trusted,
        ]

        latest = e2eControl.latestExecutorRun(runs, 7231, "a" * 40, "master")

        self.assertEqual(latest["id"], 3)

    def testLatestExecutorRunRequiresCurrentTargetRevision(self):
        run = {
            "id": 3,
            "path": ".github/workflows/build-x86-image.yaml",
            "actor": {"login": "github-actions[bot]"},
            "head_branch": "x86-e2e/pr-7231-a-1001-d-2001",
            "head_sha": "d" * 40,
            "display_title": (
                "x86-e2e pr=7231 head="
                + "a" * 40
                + " approval=1001 generation=2001 groups=policy full=0"
            ),
        }

        self.assertIsNotNone(
            e2eControl.latestExecutorRun(
                [run],
                7231,
                "a" * 40,
                "master",
                "c" * 64,
                "d" * 40,
            )
        )
        self.assertIsNone(
            e2eControl.latestExecutorRun(
                [run],
                7231,
                "a" * 40,
                "master",
                "c" * 64,
                "e" * 40,
            )
        )

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
                        "comment": {"id": 1001, "body": "/test e2e policy", "user": {"login": "maintainer"}},
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
                        "base": {"ref": "master", "sha": "b" * 40},
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
        self.assertIn("pull_request_target:\n    types: [synchronize, closed]", workflow)
        self.assertIn("push:\n    branches:", workflow)
        self.assertIn("name: Invalidate x86 E2E gates after a base update", workflow)
        self.assertIn("inputs[baseRefresh]=true", workflow)
        self.assertIn("inputs[baseSHA]=$GITHUB_SHA", workflow)
        self.assertIn("actions: write", workflow)
        self.assertIn("checks: read", workflow)
        self.assertNotIn("checks: write", workflow)
        self.assertIn("pull-requests: read", workflow)
        self.assertIn("issues: write", workflow)
        self.assertIn("--catalog trusted-catalog.json", workflow)
        self.assertIn("e2e-selection.json?ref=$baseRef", workflow)
        self.assertIn("Cancel obsolete x86 E2E executors", workflow)
        self.assertIn("live-pull-request.json", workflow)
        self.assertNotIn("Recorded x86 E2E approval", workflow)
        self.assertNotIn("Queued x86 E2E approval intent", workflow)
        self.assertIn("dispatchGate false", workflow)
        self.assertNotIn("dispatchGate true", workflow)
        self.assertIn("reservation did not become durable", workflow)
        self.assertIn("github.actor == 'github-actions[bot]'", workflow)
        self.assertIn("name: Reduce recorded x86 E2E approvals", workflow)
        self.assertIn("group: x86-e2e-reduce-${{ inputs.prNumber }}-${{ inputs.approvalGeneration }}", workflow)
        self.assertIn("needs: dispatch", workflow)
        self.assertIn("no approval was recorded", workflow)
        self.assertIn("actions/workflows/x86-e2e-gate.yaml/dispatches", workflow)
        self.assertIn("github.event_name == 'workflow_dispatch'", workflow)
        self.assertIn("inputs[baseSHA]=$BASE_SHA", workflow)
        self.assertIn("inputs[approvalGeneration]=$APPROVAL_GENERATION", workflow)
        self.assertIn("inputs[dispatchGeneration]=$DISPATCH_GENERATION", workflow)
        self.assertIn("retry the authorized command", workflow)
        self.assertIn("REQUEST_KEY: ${{ steps.request.outputs.requestKey }}", workflow)
        self.assertIn("request = e2eControl.approvedRequest(pullRequest, catalog, pages)", workflow)
        self.assertIn('e2eControl.executorRequestKey(metadata) == sys.argv[6]', workflow)
        self.assertLess(
            workflow.index("Authorized a failed-job x86 E2E rerun"),
            workflow.index("actions/runs/$runId/rerun-failed-jobs"),
        )
        self.assertIn("hasAuthorizedRerun(pages, *sys.argv[1:])", workflow)
        self.assertGreaterEqual(workflow.count("isAuthorizedRunAttempt"), 4)
        self.assertIn("pre-marker-comments.json", workflow)
        self.assertLess(
            workflow.index("pre-marker-comments.json"),
            workflow.index("Authorized a failed-job x86 E2E rerun"),
        )
        self.assertIn("start a clean trusted executor", workflow)
        self.assertIn("marker was not visible to the trusted gate", workflow)
        self.assertIn("final-rerun-pull-request.json", workflow)
        self.assertIn("final-rerun-state.json", workflow)
        self.assertIn("Another rerun consumed the authorized attempt", workflow)
        self.assertLess(
            workflow.index("final-rerun-pull-request.json"),
            workflow.index("actions/runs/$runId/rerun-failed-jobs"),
        )
        self.assertIn("rerun authorization is durable", workflow)
        self.assertNotIn("group: x86-e2e-dispatch-", workflow)
        self.assertIn("contents: write", workflow)
        self.assertIn("print(e2eControl.executorHeadBranch(request))", workflow)
        self.assertIn('git/refs/heads/$executorRef', workflow)
        self.assertIn('-f ref="$executorRef"', workflow)
        self.assertNotIn("inputs.headSHA || github.sha", workflow)

    def testGateWorkflowCanOnlyReadRunsAndWriteChecks(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-gate.yaml").read_text()

        self.assertIn("workflow_run:", workflow)
        self.assertIn("types: [completed]", workflow)
        self.assertIn("actions: read", workflow)
        self.assertIn("checks: write", workflow)
        self.assertIn("actions: write", workflow)
        self.assertIn("issues: write", workflow)
        self.assertIn("pull-requests: read", workflow)
        self.assertIn("RUN_PATH: ${{ github.event.workflow_run.path }}", workflow)
        self.assertIn("RUN_ACTOR: ${{ github.event.workflow_run.actor.login }}", workflow)
        self.assertIn("RUN_ATTEMPT: ${{ github.event.workflow_run.run_attempt }}", workflow)
        self.assertIn("RUN_TRIGGERING_ACTOR: ${{ github.event.workflow_run.triggering_actor.login }}", workflow)
        self.assertIn("currentTriggeringActor=$(jq -r", workflow)
        self.assertIn("[ \"$RUN_TRIGGERING_ACTOR\" = 'github-actions[bot]' ]", workflow)
        self.assertGreaterEqual(workflow.count("isAuthorizedRunAttempt"), 6)
        self.assertIn("serialized-run-state.json", workflow)
        self.assertIn("preserveOlderAuthorized=true", workflow)
        self.assertIn('[ "$preserveOlderAuthorized" != true ]', workflow)
        self.assertIn("latest-matching-run.json", workflow)
        self.assertIn("latestAuthorizedAttempt", workflow)
        self.assertIn("actions/runs/$matchingRunId/attempts/$candidateAttempt", workflow)
        self.assertIn("high-water-mutation.json", workflow)
        self.assertIn("high-water gate decision is not current durable coverage", workflow)
        self.assertGreaterEqual(workflow.count("for _ in $(seq 1 12)"), 2)
        self.assertIn("RUN_ATTEMPT=\"$latestAuthorizedAttempt\"", workflow)
        self.assertIn("pre-write-run-pages.json", workflow)
        self.assertIn("pre-write-comments.json", workflow)
        self.assertIn("pre-write-attempt-state.json", workflow)
        self.assertLess(
            workflow.rindex("> pre-write-run-pages.json"),
            workflow.rindex("> pre-write-comments.json"),
        )
        self.assertLess(
            workflow.rindex("> final-executor-run-pages.json"),
            workflow.rindex("> final-comments.json"),
        )
        self.assertIn(
            "group: x86-e2e-required-gate-${{ needs.gate.outputs.mutate == 'true'",
            workflow,
        )
        self.assertIn("name: Serialize x86-e2e / required-gate mutations", workflow)
        self.assertIn("cancel-in-progress: false", workflow)
        self.assertNotIn("types: [requested, completed]", workflow)
        self.assertIn("A newer authorized attempt supersedes this workflow_run event", workflow)
        self.assertIn("The latest completed executor decision artifact is not available yet", workflow)
        self.assertIn("Durable approval coverage changed before the serialized gate mutation", workflow)
        self.assertIn("liveBaseSHA=$(jq -r '.base.sha' pull-request.json)", workflow)
        self.assertIn('if [ "$BASE_REFRESH" = true ]; then', workflow)
        self.assertIn('if [ "$BASE_REFRESH" != true ] && [ -n "$duplicate" ]; then', workflow)
        self.assertIn('export BASE_SHA="$liveBaseSHA"', workflow)
        self.assertIn('metadata["executorHeadBranch"] = e2eControl.executorHeadBranch(metadata)', workflow)
        self.assertIn('[ "$RUN_HEAD_BRANCH" != "$expectedExecutorRef" ]', workflow)
        self.assertIn("authorizedAttempt = e2eControl.isAuthorizedRunAttempt", workflow)
        self.assertIn("currentBaseSHA=$(jq -r '.base.sha' final-pull-request.json)", workflow)
        self.assertIn("RUN_ACTION=base_refresh", workflow)
        self.assertIn("write-pull-request.json", workflow)
        self.assertIn("if [ \"$writeBase\" != \"$BASE_SHA\" ]; then", workflow)
        self.assertLess(
            workflow.index("write-pull-request.json"),
            workflow.index('gh api --method PATCH "repos/$GITHUB_REPOSITORY/check-runs/$checkId"'),
        )
        self.assertIn("RUN_REQUEST_KEY: ${{ needs.gate.outputs.mutate == 'true'", workflow)
        self.assertIn("inputs.baseRefresh && 'base_refresh'", workflow)
        self.assertIn("Queued x86 E2E approval intent", workflow)
        self.assertIn("Recorded x86 E2E approval", workflow)
        self.assertLess(
            workflow.index('gh api --method PATCH "repos/$GITHUB_REPOSITORY/check-runs/$checkId"'),
            workflow.index("Queued x86 E2E approval intent"),
        )
        self.assertLess(
            workflow.index('gh api --method PATCH "repos/$GITHUB_REPOSITORY/check-runs/$checkId"'),
            workflow.index("Recorded x86 E2E approval"),
        )
        self.assertGreaterEqual(workflow.count("approvalIntents"), 2)

    def testGateWorkflowRecoversTrustedTerminalDecisions(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-gate.yaml").read_text()

        self.assertIn("Unmanaged executor reruns do not change the fixed gate", workflow)
        self.assertIn("Ignoring an unmanaged newer attempt while preserving the authorized terminal result", workflow)
        self.assertIn("name: Persist the completed gate decision", workflow)
        self.assertIn("x86-e2e-gate-decision-", workflow)
        self.assertIn('[ "$RUN_ACTION" = completed ] || [ "$RUN_ACTION" = reservation ]', workflow)
        self.assertIn('[ "$recoveryRequestKey" != "$RUN_REQUEST_KEY" ]', workflow)
        self.assertIn("RUN_ACTION=completed", workflow)
        self.assertIn("recovered gate decision is not current durable coverage", workflow)
        self.assertIn(".workflow_id == $workflowId", workflow)
        self.assertIn(".path == \".github/workflows/x86-e2e-gate.yaml\"", workflow)
        self.assertIn(".event == \"workflow_run\"", workflow)
        self.assertIn(".actor.login == \"github-actions[bot]\"", workflow)
        self.assertIn("trusted-gate-run-pages.json", workflow)
        self.assertIn("actions/runs/$ownerRunId/artifacts?per_page=100", workflow)
        self.assertNotIn("actions/artifacts?name=$artifactName", workflow)
        self.assertIn("recovered gate decision has an invalid conclusion", workflow)
        self.assertIn("name: Reconcile a durable x86 E2E approval", workflow)
        self.assertIn("actions/workflows/x86-e2e-dispatcher.yaml/dispatches", workflow)
        self.assertIn("Download the executed SelectionPlan", workflow)
        self.assertIn("attempt <= currentAttempt", workflow)
        self.assertIn("executed-selection-plan.json", workflow)
        self.assertIn("expectedCount >= 3000", workflow)
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
        self.assertIn(
            "GH_TOKEN: ${{ github.event_name == 'workflow_dispatch' && github.token || '' }}",
            workflow,
        )
        self.assertIn("unknown requested E2E group", workflow)
        self.assertIn("executor catalog revision does not match dispatcher", workflow)
        self.assertIn("executor workflow revision does not match the approved base", workflow)
        self.assertIn("trusted selector checkout does not match the approved base revision", workflow)
        self.assertIn("ref: ${{ github.event_name == 'workflow_dispatch' && inputs.baseSHA", workflow)
        self.assertGreaterEqual(workflow.count("github.actor == 'github-actions[bot]'"), 6)
        self.assertGreaterEqual(workflow.count("needs: e2e-selection"), 3)
        self.assertNotIn('entry["selection"] != "smoke"', workflow)
        self.assertIn("contents: read", workflow)
        self.assertIn("packages: read", workflow)
        self.assertIn("-u ACTIONS_RUNTIME_TOKEN", workflow)
        self.assertIn("-u ACTIONS_RESULTS_URL", workflow)
        self.assertIn("-u LD_PRELOAD", workflow)
        self.assertIn("realEnv=os.environ['GITHUB_ENV']", workflow)
        self.assertIn("os.environ['GITHUB_ENV']=shadowDir+'/env'", workflow)
        self.assertIn("allowed=('TAG','GO_VERSION','E2E_DIR','VERSION','DEBUG_WRAPPER')", workflow)
        self.assertNotIn("actions: write", workflow)
        self.assertNotIn("checks: write", workflow)
        self.assertIn("GHCR_TOKEN: ${{ secrets.GITHUB_TOKEN }}", workflow)
        self.assertNotIn(
            "GHCR_TOKEN: ${{ github.event_name == 'push' && secrets.GITHUB_TOKEN || '' }}",
            workflow,
        )
        self.assertIn("Initialize fail-closed conformance matrices", workflow)
        self.assertIn("steps.fallbackMatrices.outputs.k8sConformanceMatrix", workflow)
        self.assertIn(
            "matrix: ${{ fromJSON(needs.e2e-selection.outputs.k8sConformanceMatrix) }}",
            blocks["k8s-conformance-e2e"],
        )
        self.assertIn(
            "matrix: ${{ fromJSON(needs.e2e-selection.outputs.kubeOvnConformanceMatrix) }}",
            blocks["kube-ovn-conformance-e2e"],
        )
        self.assertIn(
            "if: github.event_name == 'push'\n        uses: actions/cache@v6",
            workflow,
        )
        self.assertIn("--force-full-reason \"$FORCE_FULL_REASON\"", workflow)
        self.assertIn("expectedCount >= 3000", workflow)
        self.assertIn("e2eSelector.expandWorkflow(workflow)", workflow)
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
                self.assertIn("persist-credentials: false", block)
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
