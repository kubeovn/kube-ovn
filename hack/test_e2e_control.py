#!/usr/bin/env python3

import json
import re
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
        headSHA = "a" * 40
        nonce = "b" * 16
        cases = {
            f"/test e2e --head {headSHA} --nonce {nonce}": ("dispatch", [], False),
            f"/test e2e policy,multi-cni --head {headSHA} --nonce {nonce}": (
                "dispatch",
                ["multi-cni", "policy"],
                False,
            ),
            f"/test e2e-all --head {headSHA} --nonce {nonce}": ("dispatch", [], True),
            f"/retest e2e-failed --head {headSHA} --nonce {nonce}": (
                "rerun-failed",
                [],
                False,
            ),
        }

        for body, expected in cases.items():
            with self.subTest(body=body):
                command = e2eControl.parseCommand(body)
                self.assertEqual(
                    (command["action"], command["requestedGroups"], command["full"]),
                    expected,
                )
                self.assertEqual(command["headSHA"], headSHA)
                self.assertEqual(command["nonce"], nonce)

    def testParsesUnboundCommentCommands(self):
        cases = {
            "/test e2e": ("dispatch", [], False),
            "/test e2e core": ("dispatch", ["core"], False),
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
                self.assertEqual(command["headSHA"], "")
                self.assertEqual(command["nonce"], "")

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
        body=None,
        permission="write",
        observedHeadSHA="a" * 40,
        confirmedHeadSHA="a" * 40,
        state="open",
        baseRef="master",
        controlledLabels=(),
        bindHead=True,
    ):
        catalog = json.loads((repoRoot / ".github/e2e-selection.json").read_text())
        catalogRevision = e2eSelector.catalogRevision(catalog)
        baseSHA = "b" * 40
        if body is None:
            nonce = e2eControl.requestNonce(7231, observedHeadSHA, baseSHA, catalogRevision)
            body = f"/test e2e policy --head {observedHeadSHA} --nonce {nonce}"
        elif bindHead and " --head " not in body:
            nonce = e2eControl.requestNonce(7231, observedHeadSHA, baseSHA, catalogRevision)
            body = f"{body} --head {observedHeadSHA} --nonce {nonce}"
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
            "base": {"ref": baseRef, "sha": baseSHA},
        }
        return e2eControl.decideDispatch(
            event,
            permission,
            pullRequest,
            confirmedHeadSHA,
            set(catalog["groups"]),
            catalogRevision,
            liveComment=event["comment"],
            controlledLabels=controlledLabels,
        )

    def testExecutorJobsArePublishedAsPullRequestChecks(self):
        jobs = [
            {
                "id": 1,
                "name": "Kubernetes Conformance E2E (ipv4, overlay)",
                "status": "completed",
                "conclusion": "success",
                "html_url": "https://github.com/kubeovn/kube-ovn/actions/runs/1/job/11",
            },
            {
                "id": 2,
                "name": "Build kube-ovn",
                "status": "completed",
                "conclusion": "success",
                "html_url": "https://github.com/kubeovn/kube-ovn/actions/runs/1/job/12",
            },
            {
                "id": 3,
                "name": "Kube-OVN Hosted OVN Central E2E (ipv4, 1 control-plane)",
                "status": "in_progress",
                "conclusion": None,
                "html_url": "https://github.com/kubeovn/kube-ovn/actions/runs/1/job/13",
            },
        ]
        selected = e2eControl.selectedExecutorJobs(
            jobs,
            [
                "Kubernetes Conformance E2E",
                "Kube-OVN Hosted OVN Central E2E (${{ matrix.ip-family }}, ${{ matrix.tenant-control-plane }} control-plane)",
            ],
        )
        self.assertEqual([job["id"] for job in selected], [1, 3])
        headSHA = "a" * 40
        completed = e2eControl.prCheckRunFromExecutorJob(jobs[0], headSHA, 7260)
        pending = e2eControl.prCheckRunFromExecutorJob(jobs[2], headSHA, 7260)
        self.assertEqual(completed["head_sha"], headSHA)
        self.assertEqual(completed["status"], "completed")
        self.assertEqual(completed["conclusion"], "success")
        self.assertEqual(completed["details_url"], jobs[0]["html_url"])
        self.assertEqual(pending["status"], "in_progress")
        self.assertNotIn("conclusion", pending)
        self.assertFalse(
            e2eControl.selectedExecutorJobsAreTerminal(
                jobs,
                [
                    "Kubernetes Conformance E2E",
                    "Kube-OVN Hosted OVN Central E2E (${{ matrix.ip-family }}, ${{ matrix.tenant-control-plane }} control-plane)",
                ],
            )
        )
        self.assertTrue(
            e2eControl.selectedExecutorJobsAreTerminal(
                [jobs[0]],
                ["Kubernetes Conformance E2E"],
            )
        )
        self.assertFalse(
            e2eControl.selectedExecutorJobsAreTerminal(
                [],
                ["Kubernetes Conformance E2E"],
            )
        )

    def testUnstartedSelectedJobsArePublishedAsQueuedPlaceholders(self):
        jobs = [
            {
                "id": 10,
                "name": "Build kube-ovn",
                "status": "in_progress",
                "conclusion": None,
                "html_url": "https://github.com/kubeovn/kube-ovn/actions/runs/1/job/10",
            }
        ]
        selectedTitles = [
            "Kubernetes Conformance E2E (${{ matrix.ip-family }}, ${{ matrix.mode }})",
            "Kube-OVN Conformance E2E (${{ matrix.ip-family }}, ${{ matrix.mode }})",
            "Cilium Chaining E2E",
        ]
        payloads = e2eControl.prCheckRunsToPublish(
            jobs,
            selectedTitles,
            "a" * 40,
            7275,
            detailsURL="https://github.com/kubeovn/kube-ovn/actions/runs/1",
            infraTitles=[
                "Build kube-ovn",
                "Build E2E Binaries",
                "Prepare private Kind node image (${{ matrix.k8s-version }})",
            ],
        )
        byName = {payload["name"]: payload for payload in payloads}
        self.assertEqual(
            [payload["name"] for payload in payloads],
            [
                "Build kube-ovn",
                "Kubernetes Conformance E2E",
                "Kube-OVN Conformance E2E",
                "Cilium Chaining E2E",
            ],
        )
        self.assertEqual(byName["Build kube-ovn"]["status"], "in_progress")
        self.assertEqual(byName["Build kube-ovn"]["head_sha"], "a" * 40)
        self.assertEqual(
            byName["Build kube-ovn"]["details_url"],
            jobs[0]["html_url"],
        )
        self.assertEqual(byName["Kubernetes Conformance E2E"]["status"], "queued")
        self.assertNotIn("conclusion", byName["Kubernetes Conformance E2E"])
        self.assertEqual(
            byName["Kubernetes Conformance E2E"]["details_url"],
            "https://github.com/kubeovn/kube-ovn/actions/runs/1",
        )
        self.assertEqual(byName["Cilium Chaining E2E"]["status"], "queued")
        self.assertTrue(
            byName["Kubernetes Conformance E2E"]["external_id"].startswith(
                "x86-e2e-pr-7275-"
            )
        )

    def testMatrixPlaceholdersCompleteAfterRealJobsStart(self):
        jobs = [
            {
                "id": 2,
                "name": "Build kube-ovn",
                "status": "completed",
                "conclusion": "success",
                "html_url": "https://github.com/kubeovn/kube-ovn/actions/runs/1/job/12",
            },
            {
                "id": 4,
                "name": "Kubernetes Conformance E2E (ipv4, overlay)",
                "status": "in_progress",
                "conclusion": None,
                "html_url": "https://github.com/kubeovn/kube-ovn/actions/runs/1/job/14",
            },
            {
                "id": 5,
                "name": "Cilium Chaining E2E",
                "status": "queued",
                "conclusion": None,
                "html_url": "https://github.com/kubeovn/kube-ovn/actions/runs/1/job/15",
            },
        ]
        payloads = e2eControl.prCheckRunsToPublish(
            jobs,
            [
                "Kubernetes Conformance E2E (${{ matrix.ip-family }}, ${{ matrix.mode }})",
                "Cilium Chaining E2E",
            ],
            "a" * 40,
            7275,
            detailsURL="https://github.com/kubeovn/kube-ovn/actions/runs/1",
            infraTitles=["Build kube-ovn"],
        )
        byName = {payload["name"]: payload for payload in payloads}
        self.assertEqual(byName["Build kube-ovn"]["status"], "completed")
        self.assertEqual(byName["Build kube-ovn"]["conclusion"], "success")
        self.assertEqual(
            byName["Kubernetes Conformance E2E (ipv4, overlay)"]["status"],
            "in_progress",
        )
        self.assertEqual(byName["Kubernetes Conformance E2E"]["status"], "completed")
        self.assertEqual(byName["Kubernetes Conformance E2E"]["conclusion"], "skipped")
        self.assertEqual(byName["Cilium Chaining E2E"]["status"], "queued")
        self.assertNotIn("conclusion", byName["Cilium Chaining E2E"])
        self.assertEqual(
            [payload["name"] for payload in payloads].count("Cilium Chaining E2E"),
            1,
        )

    def testApprovedGateReservationIgnoresGitHubRewrittenDetailsURL(self):
        headSHA = "94fd288b1db022d10863ac3254d2b40fe1dad01a"
        check = {
            "name": "x86-e2e / required-gate",
            "external_id": f"x86-e2e-pr-7260-{headSHA}",
            "status": "completed",
            "conclusion": "action_required",
            "details_url": "https://github.com/kubeovn/kube-ovn/runs/96297378780",
            "output": {
                "title": "x86 E2E gate",
                "summary": "The latest authorized x86 E2E approval is waiting for its trusted executor.",
            },
        }

        self.assertTrue(e2eControl.isApprovedGateReservation(check, 7260, headSHA))
        self.assertFalse(
            e2eControl.isApprovedGateReservation(
                {
                    **check,
                    "output": {
                        "summary": "The pull request target branch advanced; authorize x86 E2E again for the new base revision."
                    },
                },
                7260,
                headSHA,
            )
        )

    def testUnboundCommandBindsToLiveHead(self):
        decision = self.dispatchDecision(body="/test e2e core", bindHead=False)

        self.assertTrue(decision["accepted"])
        self.assertEqual(decision["action"], "dispatch")
        self.assertEqual(decision["requestedGroups"], ["core"])
        self.assertEqual(decision["headSHA"], "a" * 40)
        self.assertEqual(decision["baseSHA"], "b" * 40)
        self.assertRegex(decision["nonce"], r"^[0-9a-f]{16}$")

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

    def testCommentBindingCannotBeReplayedForAnotherHead(self):
        catalog = json.loads((repoRoot / ".github/e2e-selection.json").read_text())
        nonce = e2eControl.requestNonce(
            7231,
            "a" * 40,
            "b" * 40,
            e2eSelector.catalogRevision(catalog),
        )
        decision = self.dispatchDecision(
            body=f"/test e2e --head {'a' * 40} --nonce {nonce}",
            observedHeadSHA="c" * 40,
            confirmedHeadSHA="c" * 40,
        )

        self.assertFalse(decision["accepted"])
        self.assertEqual(decision["reason"], "comment is bound to another pull request HEAD")

    def testEditedOrDeletedCommentEventCannotBeReplayed(self):
        catalog = json.loads((repoRoot / ".github/e2e-selection.json").read_text())
        catalogRevision = e2eSelector.catalogRevision(catalog)
        nonce = e2eControl.requestNonce(7231, "a" * 40, "b" * 40, catalogRevision)
        event = {
            "action": "created",
            "comment": {
                "id": 1001,
                "body": f"/test e2e --head {'a' * 40} --nonce {nonce}",
                "user": {"login": "maintainer"},
            },
            "issue": {"number": 7231, "pull_request": {}},
            "repository": {"full_name": "kubeovn/kube-ovn"},
        }
        pullRequest = {
            "number": 7231,
            "state": "open",
            "head": {"sha": "a" * 40},
            "base": {"ref": "master", "sha": "b" * 40},
        }

        decision = e2eControl.decideDispatch(
            event,
            "write",
            pullRequest,
            "a" * 40,
            set(catalog["groups"]),
            catalogRevision,
            liveComment={**event["comment"], "body": "edited"},
        )

        self.assertFalse(decision["accepted"])
        self.assertEqual(decision["reason"], "comment changed or was deleted before dispatch")

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

    def testTrustedControlledLabelsCanOnlyAddApprovedCoverage(self):
        grouped = self.dispatchDecision(
            body="/test e2e",
            controlledLabels=["e2e:multi-cni", "e2e:policy"],
        )
        full = self.dispatchDecision(
            body="/test e2e policy",
            controlledLabels=["e2e:full"],
        )

        self.assertEqual(grouped["requestedGroups"], ["multi-cni", "policy"])
        self.assertTrue(full["full"])
        self.assertEqual(full["requestedGroups"], [])

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

    def testControlledLabelsRequireLatestTrustedBotMarkerAndLivePresence(self):
        catalog = e2eSelector.loadCatalog(repoRoot / ".github/e2e-selection.json")
        policyPresent = e2eControl.renderControlledLabelMarker("e2e:policy", True)
        policyRemoved = e2eControl.renderControlledLabelMarker("e2e:policy", False)
        fullPresent = e2eControl.renderControlledLabelMarker("e2e:full", True)
        pages = [[
            {
                "id": 10,
                "user": {"login": "github-actions[bot]", "type": "Bot"},
                "body": policyPresent,
            },
            {
                "id": 11,
                "user": {"login": "attacker", "type": "User"},
                "body": fullPresent,
            },
            {
                "id": 12,
                "user": {"login": "github-actions[bot]", "type": "Bot"},
                "body": policyRemoved,
            },
            {
                "id": 13,
                "user": {"login": "github-actions[bot]", "type": "Bot"},
                "body": fullPresent,
            },
        ]]

        labels = e2eControl.trustedControlledLabels(
            catalog,
            pages,
            ["e2e:policy", "e2e:full", "e2e:unknown"],
        )

        self.assertEqual(labels, ["e2e:full"])
        self.assertEqual(
            e2eControl.parseControlledLabelMarker(fullPresent),
            {"label": "e2e:full", "present": True},
        )

    def testApprovalMarkerIsInvalidatedWhenTrustedLabelsChange(self):
        catalog = e2eSelector.loadCatalog(repoRoot / ".github/e2e-selection.json")
        pullRequest = {
            "number": 7231,
            "head": {"sha": "a" * 40},
            "base": {"ref": "master", "sha": "b" * 40},
            "labels": [{"name": "e2e:policy"}],
        }
        catalogRevision = e2eSelector.catalogRevision(catalog)
        pages = [[
            {
                "id": 10,
                "user": {"login": "github-actions[bot]", "type": "Bot"},
                "body": e2eControl.renderControlledLabelMarker("e2e:policy", True),
            },
            {
                "id": 11,
                "user": {"login": "github-actions[bot]", "type": "Bot"},
                "body": e2eControl.renderRequestMarker(
                    {
                        "headSHA": "a" * 40,
                        "baseSHA": "b" * 40,
                        "approvalGeneration": 1001,
                        "catalogRevision": catalogRevision,
                        "requestedGroups": ["policy"],
                        "full": False,
                    }
                ),
            },
        ]]

        self.assertIsNone(e2eControl.approvedRequest(pullRequest, catalog, pages))

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
            + " approval=1001 generation=2001 mode=approved groups=multi-cni,policy labels=- full=0"
        )

        self.assertEqual(metadata["prNumber"], 7231)
        self.assertEqual(metadata["headSHA"], "a" * 40)
        self.assertEqual(metadata["approvalGeneration"], 1001)
        self.assertEqual(metadata["dispatchGeneration"], 2001)
        self.assertEqual(metadata["requestedGroups"], ["multi-cni", "policy"])
        self.assertEqual(metadata["controlledLabels"], [])
        self.assertFalse(metadata["automatic"])
        self.assertFalse(metadata["full"])

    def testParsesTrustedAutomaticExecutorRunName(self):
        metadata = e2eControl.parseExecutorRunName(
            "x86-e2e pr=7231 head="
            + "a" * 40
            + " approval=1 generation=2001 mode=automatic groups=- labels=e2e:policy full=0"
        )
        metadata["baseSHA"] = "b" * 40
        metadata["catalogRevision"] = "c" * 64

        self.assertTrue(metadata["automatic"])
        self.assertEqual(metadata["controlledLabels"], ["e2e:policy"])
        self.assertEqual(e2eControl.executorRequestKey(metadata), "")

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

    def testIsolatedExecutorRefActionTreatsMissingGitHubRefAsCreate(self):
        expected = "e" * 40
        missingRef = json.dumps(
            {
                "message": "Not Found",
                "documentation_url": "https://docs.github.com/rest/git/refs#get-a-reference",
                "status": "404",
            }
        )

        self.assertEqual(e2eControl.isolatedExecutorRefAction("", expected), "create")
        self.assertEqual(e2eControl.isolatedExecutorRefAction(missingRef, expected), "create")
        self.assertEqual(e2eControl.isolatedExecutorRefAction("null", expected), "create")
        self.assertEqual(
            e2eControl.isolatedExecutorRefAction({"object": {"sha": expected}}, expected),
            "reuse",
        )
        self.assertEqual(
            e2eControl.isolatedExecutorRefAction({"object": {"sha": "f" * 40}}, expected),
            "reject",
        )

    def testTrustedExecutorRefAcceptsIsolatedBranchName(self):
        request = {
            "prNumber": 7251,
            "approvalGeneration": 1,
            "dispatchGeneration": 32235889723,
        }
        isolated = "x86-e2e/pr-7251-a-1-d-32235889723"

        self.assertTrue(e2eControl.isTrustedExecutorRef("master", "master", request))
        self.assertTrue(e2eControl.isTrustedExecutorRef(isolated, "master", request))
        self.assertFalse(e2eControl.isTrustedExecutorRef("ci-e2e-demand-gate", "master", request))
        self.assertFalse(
            e2eControl.isTrustedExecutorRef(
                "x86-e2e/pr-7251-a-1-d-1",
                "master",
                request,
            )
        )

    def testRejectsMalformedExecutorRunName(self):
        with self.assertRaisesRegex(ValueError, "invalid x86 E2E executor run name"):
            e2eControl.parseExecutorRunName(
                "x86-e2e pr=7231 head="
                + "a" * 40
                + " approval=0 generation=2001 mode=approved groups=multi-cni,policy labels=- full=0"
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
            + " approval=1001 generation=2001 mode=approved groups=policy labels=- full=0"
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
                + " approval=1001 generation=2001 mode=approved groups=policy labels=- full=0"
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

    def testLatestExecutorRunPrefersApprovedCoverageOverAutomaticProbe(self):
        head = "a" * 40
        base = "d" * 40
        automatic = {
            "id": 9,
            "path": ".github/workflows/build-x86-image.yaml",
            "actor": {"login": "github-actions[bot]"},
            "head_branch": "x86-e2e/pr-7231-a-1-d-2002",
            "head_sha": base,
            "display_title": (
                "x86-e2e pr=7231 head=" + head
                + " approval=1 generation=2002 mode=automatic groups=- labels=- full=0"
            ),
        }
        approved = {
            **automatic,
            "id": 8,
            "head_branch": "x86-e2e/pr-7231-a-1-d-2001",
            "display_title": (
                "x86-e2e pr=7231 head=" + head
                + " approval=1 generation=2001 mode=approved groups=policy labels=- full=0"
            ),
        }

        latest = e2eControl.latestExecutorRun(
            [automatic, approved], 7231, head, "master", None, base
        )

        self.assertEqual(latest["id"], 8)

    def testInProgressAutomaticExecutorsForTheSameHeadAreCancelled(self):
        head = "a" * 40
        otherHead = "b" * 40
        runs = [
            {
                "id": 11,
                "path": ".github/workflows/build-x86-image.yaml",
                "actor": {"login": "github-actions[bot]"},
                "status": "in_progress",
                "display_title": (
                    "x86-e2e pr=7278 head=" + head
                    + " approval=1 generation=1001 mode=automatic groups=- labels=- full=0"
                ),
            },
            {
                "id": 12,
                "path": ".github/workflows/build-x86-image.yaml",
                "actor": {"login": "github-actions[bot]"},
                "status": "queued",
                "display_title": (
                    "x86-e2e pr=7278 head=" + head
                    + " approval=1 generation=1002 mode=automatic groups=- labels=- full=0"
                ),
            },
            {
                "id": 13,
                "path": ".github/workflows/build-x86-image.yaml",
                "actor": {"login": "github-actions[bot]"},
                "status": "in_progress",
                "display_title": (
                    "x86-e2e pr=7278 head=" + head
                    + " approval=1 generation=1003 mode=approved groups=core labels=- full=0"
                ),
            },
            {
                "id": 14,
                "path": ".github/workflows/build-x86-image.yaml",
                "actor": {"login": "github-actions[bot]"},
                "status": "in_progress",
                "display_title": (
                    "x86-e2e pr=7278 head=" + otherHead
                    + " approval=1 generation=1004 mode=automatic groups=- labels=- full=0"
                ),
            },
            {
                "id": 15,
                "path": ".github/workflows/build-x86-image.yaml",
                "actor": {"login": "github-actions[bot]"},
                "status": "completed",
                "display_title": (
                    "x86-e2e pr=7278 head=" + head
                    + " approval=1 generation=1005 mode=automatic groups=- labels=- full=0"
                ),
            },
        ]

        self.assertEqual(
            e2eControl.inProgressAutomaticExecutorRunIds(runs, 7278, head),
            [11, 12],
        )

    def testDispatchCliWritesDecisionJson(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            event = directory / "event.json"
            pullRequest = directory / "pr.json"
            liveComment = directory / "comment.json"
            controlledLabels = directory / "controlled-labels.json"
            decision = directory / "decision.json"
            event.write_text(
                json.dumps(
                    {
                        "action": "created",
                        "comment": {
                            "id": 1001,
                            "body": (
                                "/test e2e policy --head "
                                + "a" * 40
                                + " --nonce "
                                + e2eControl.requestNonce(
                                    7231,
                                    "a" * 40,
                                    "b" * 40,
                                    e2eSelector.catalogRevision(
                                        e2eSelector.loadCatalog(
                                            repoRoot / ".github/e2e-selection.json"
                                        )
                                    ),
                                )
                            ),
                            "user": {"login": "maintainer"},
                        },
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
            liveComment.write_text(json.dumps(json.loads(event.read_text())["comment"]))
            controlledLabels.write_text("[]")

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
                    "--live-comment-file",
                    str(liveComment),
                    "--controlled-labels-file",
                    str(controlledLabels),
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
        self.assertIn(
            "pull_request_target:\n    types: [opened, reopened, synchronize, labeled, unlabeled, closed]",
            workflow,
        )
        self.assertIn("push:\n    branches:", workflow)
        self.assertIn("name: Invalidate x86 E2E gates after a base update", workflow)
        self.assertIn("inputs[baseRefresh]=true", workflow)
        self.assertIn("inputs[baseSHA]=$GITHUB_SHA", workflow)
        self.assertIn("actions: write", workflow)
        self.assertIn("checks: read", workflow)
        self.assertNotIn("checks: write", workflow)
        self.assertIn("pull-requests: read", workflow)
        self.assertIn("pull-requests: write", workflow)
        self.assertIn("issues: write", workflow)
        self.assertIn("--catalog trusted-catalog.json", workflow)
        self.assertIn("--live-comment-file live-comment.json", workflow)
        self.assertIn("issues/comments/$COMMENT_ID", workflow)
        self.assertIn("e2e-selection.json?ref=$baseRef", workflow)
        self.assertIn("Cancel obsolete x86 E2E executors", workflow)
        self.assertIn("Record trusted x86 E2E controlled labels", workflow)
        self.assertIn("collaborators/$SENDER/permission", workflow)
        self.assertIn("renderControlledLabelMarker", workflow)
        self.assertIn("trustedControlledLabels", workflow)
        self.assertIn("labels/$encodedLabel", workflow)
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
        self.assertIn("isApprovedGateReservation", workflow)
        self.assertNotIn(".details_url == $expectedURL", workflow)
        self.assertGreaterEqual(workflow.count("isolatedExecutorRefAction"), 2)
        self.assertNotIn("--jq '.object.sha'", workflow)
        self.assertIn('git/refs/heads/$executorRef', workflow)
        self.assertIn('-f ref="$executorRef"', workflow)
        self.assertNotIn("ref: ${{ inputs.headSHA || github.sha }}", workflow)

    def testAutomaticCoverageIgnoresUncontrolledLabelEvents(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-dispatcher.yaml").read_text()
        automatic = e2eSelector.workflowJobBlocks(workflow)["automatic"]

        self.assertIn("github.event.action == 'opened'", automatic)
        self.assertIn("github.event.action == 'reopened'", automatic)
        self.assertIn("github.event.action == 'synchronize'", automatic)
        self.assertIn("github.event.action == 'labeled'", automatic)
        self.assertIn("github.event.action == 'unlabeled'", automatic)
        self.assertIn("startsWith(github.event.label.name, 'e2e:')", automatic)
        self.assertNotIn("github.event.action != 'closed'", automatic)
        self.assertIn(
            "group: x86-e2e-automatic-${{ github.event.pull_request.number }}-"
            "${{ github.event.pull_request.head.sha }}",
            automatic,
        )
        self.assertIn("cancel-in-progress: true", automatic)
        self.assertIn("inProgressAutomaticExecutorRunIds", automatic)
        self.assertIn("actions/runs/$runId/cancel", automatic)
        self.assertIn('requestKey="automatic-$PR_NUMBER-$headSHA"', automatic)
        self.assertNotIn('requestKey="automatic-$DISPATCH_GENERATION"', automatic)

    def testGateWorkflowCanOnlyReadRunsAndWriteChecks(self):
        workflow = (repoRoot / ".github/workflows/x86-e2e-gate.yaml").read_text()

        self.assertIn("workflow_run:", workflow)
        self.assertIn("types: [completed]", workflow)
        self.assertIn("actions: read", workflow)
        self.assertIn("checks: write", workflow)
        self.assertIn("actions: write", workflow)
        self.assertIn("issues: write", workflow)
        self.assertIn("pull-requests: write", workflow)
        self.assertIn("RUN_PATH: ${{ github.event.workflow_run.path }}", workflow)
        self.assertIn("RUN_ACTOR: ${{ github.event.workflow_run.actor.login }}", workflow)
        self.assertIn("RUN_ATTEMPT: ${{ github.event.workflow_run.run_attempt }}", workflow)
        self.assertIn("RUN_TRIGGERING_ACTOR: ${{ github.event.workflow_run.triggering_actor.login }}", workflow)
        self.assertIn(
            "RUN_HEAD_REPOSITORY: ${{ github.event.workflow_run.head_repository.full_name }}",
            workflow,
        )
        self.assertIn('-f "head=$headOwner:$RUN_HEAD_BRANCH"', workflow)
        self.assertIn("workflow HEAD must resolve to exactly one open pull request", workflow)
        self.assertIn('.head.repo.full_name == $headRepository', workflow)
        self.assertIn('.base.repo.full_name == $repository', workflow)
        self.assertIn("automatic: ${{ steps.context.outputs.automatic }}", workflow)
        self.assertIn("controlledLabels: ${{ steps.context.outputs.controlledLabels }}", workflow)
        self.assertIn("A trusted comment approval supersedes this automatic executor.", workflow)
        self.assertIn("Controlled label provenance changed; the automatic executor is stale.", workflow)
        self.assertIn("CONTROLLED_LABELS: ${{ steps.context.outputs.controlledLabels }}", workflow)
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

    def testWorkflowDispatchInputsUseGitHubRESTStringFields(self):
        for workflowName in ["x86-e2e-dispatcher.yaml", "x86-e2e-gate.yaml"]:
            with self.subTest(workflow=workflowName):
                workflow = (repoRoot / ".github/workflows" / workflowName).read_text()
                typedInputs = re.findall(r"-F\s+['\"]inputs\[", workflow)

                self.assertEqual(
                    typedInputs,
                    [],
                    "workflow_dispatch REST inputs must use -f so GitHub can "
                    "validate and coerce them using the target workflow schema",
                )

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
        self.assertIn("args+=(--request-full)", workflow)
        self.assertNotIn("args+=(--label e2e:full)", workflow)
        self.assertIn(
            'e2eSelector.executionPlan(catalog, plan, os.environ["EXECUTION_EVENT"])',
            workflow,
        )
        self.assertIn("EXECUTION_EVENT: ${{ steps.context.outputs.approved == 'true'", workflow)
        self.assertIn("ref: ${{ github.event.repository.default_branch }}", workflow)
        self.assertIn("ref: ${{ steps.context.outputs.trustedRef }}", workflow)
        self.assertNotIn("ref: ${{ steps.context.outputs.baseRef }}", workflow)
        self.assertNotIn("ref: ${{ inputs.headSHA || github.sha }}", workflow)
        self.assertNotIn("contents: write", workflow)

    def testExecutorWorkflowUsesUnprivilegedExactHeadContext(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()

        self.assertIn("workflow_dispatch:", workflow)
        self.assertIn("automatic:", workflow)
        self.assertIn("controlledLabels:", workflow)
        self.assertIn("inputs.automatic && 'automatic'", workflow)
        self.assertIn("EVENT_NAME: ${{ inputs.automatic && 'pull_request' || github.event_name }}", workflow)
        self.assertIn(
            "GH_TOKEN: ${{ github.event_name == 'workflow_dispatch' && github.token || '' }}",
            workflow,
        )
        self.assertIn("unknown requested E2E group", workflow)
        self.assertIn("executor catalog revision does not match dispatcher", workflow)
        self.assertIn("executor workflow revision does not match the approved base", workflow)
        self.assertIn("isTrustedExecutorRef", workflow)
        self.assertNotIn(
            'if pullRequest["base"]["ref"] != sys.argv[3]:',
            workflow,
        )
        self.assertIn("trusted selector checkout does not match the approved base revision", workflow)
        self.assertIn("ref: ${{ github.event_name == 'workflow_dispatch' && inputs.baseSHA", workflow)
        self.assertGreaterEqual(workflow.count("github.actor == 'github-actions[bot]'"), 5)
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
        self.assertEqual(workflow.count("checks: write"), 1)
        self.assertIn("name: Publish x86 E2E checks on the pull request", workflow)
        self.assertNotIn("GHCR_TOKEN: ${{ secrets.GITHUB_TOKEN }}", workflow)
        self.assertIn(
            "Pull private Kind node image with trusted token",
            workflow,
        )
        self.assertIn("kind-node-v1.36.1.tar", workflow)
        self.assertIn("kind-node-v1.29.14.tar", workflow)
        self.assertNotIn("kind-ghcr-pull", workflow)
        self.assertIn(
            "EXECUTION_SHA: ${{ github.event_name == 'pull_request' && "
            "github.event.pull_request.head.sha || inputs.headSHA || github.sha }}",
            workflow,
        )
        self.assertNotIn("ref: ${{ inputs.headSHA || github.sha }}", workflow)
        self.assertNotIn("github.event.pull_request.head.sha || inputs.headSHA", workflow.replace(
            "EXECUTION_SHA: ${{ github.event_name == 'pull_request' && "
            "github.event.pull_request.head.sha || inputs.headSHA || github.sha }}",
            "",
        ))
        self.assertGreaterEqual(workflow.count("ref: ${{ env.EXECUTION_SHA }}"), 31)
        self.assertIn(
            '"${{ github.event.pull_request.base.sha }}...${{ github.event.pull_request.head.sha }}"',
            workflow,
        )

    def testPullRequestKeepsBaselineBuildWithoutKindImageGate(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        blocks = e2eSelector.workflowJobBlocks(workflow)
        catalog = e2eSelector.loadCatalog(repoRoot / ".github/e2e-selection.json")
        testJobs = {
            job["id"]
            for group in catalog["groups"].values()
            for job in group["jobs"]
        }
        build = blocks["build-kube-ovn"]

        self.assertIn("make ut", build)
        self.assertIn("make lint", build)
        self.assertIn("make image-kube-ovn", build)
        self.assertNotIn("- prepare-kind-node-images", build)
        self.assertIn(
            "if: github.event_name != 'workflow_dispatch' || github.actor == 'github-actions[bot]'",
            build,
        )
        for jobId in sorted(testJobs):
            with self.subTest(jobId=jobId):
                block = blocks[jobId]
                self.assertIn(
                    "github.event_name != 'pull_request' && "
                    "contains(fromJSON(needs.e2e-selection.outputs.executionJobIds)",
                    block,
                )
                if "docker load --input kind-node-" in block:
                    self.assertIn("- prepare-kind-node-images", block)

    def testPullRequestsExecuteAutomaticCoverageAndPushStaysFull(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        blocks = e2eSelector.workflowJobBlocks(workflow)
        catalog = e2eSelector.loadCatalog(repoRoot / ".github/e2e-selection.json")
        testJobs = {
            job["id"]
            for group in catalog["groups"].values()
            for job in group["jobs"]
        }

        self.assertGreaterEqual(workflow.count("needs: e2e-selection"), 2)
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
        self.assertIn("if: github.event_name == 'push'\n        uses: actions/cache@v6", workflow)
        self.assertIn("--force-full-reason \"$FORCE_FULL_REASON\"", workflow)
        self.assertIn("--request-full", workflow)
        self.assertNotIn("args+=(--label e2e:full)", workflow)
        self.assertIn("expectedCount >= 3000", workflow)
        self.assertIn(
            'e2eSelector.executionPlan(catalog, plan, os.environ["EVENT_NAME"])',
            workflow,
        )
        self.assertIn("except Exception as error:", workflow)
        self.assertIn("failed to reload the E2E catalog", workflow)
        self.assertIn(
            '"e2e-selection-plan.json",\n              "e2e-selection-summary.md",',
            workflow,
        )
        self.assertNotIn("continue-on-error:", blocks["e2e-selection"])
        self.assertNotIn("e2eSelector.expandWorkflow(workflow)", workflow)
        validationBlock = blocks["e2e-control-validation"]
        self.assertIn("permissions:\n      contents: read", validationBlock)
        self.assertIn("python3 -m unittest hack/test_e2e_selector.py hack/test_e2e_control.py", validationBlock)
        resultBlock = blocks["e2e-executor-result"]
        self.assertIn("if: always() && github.event_name != 'pull_request'", resultBlock)
        self.assertIn("permissions: {}", resultBlock)
        publish = blocks["publish-pr-e2e-checks"]
        self.assertIn("if: github.event_name == 'workflow_dispatch'", publish)
        self.assertIn("checks: write", publish)
        self.assertIn("prCheckRunsToPublish", publish)
        self.assertIn("visibleInfrastructureTitles", publish)
        self.assertIn("selectedExecutorJobsAreTerminal", publish)
        self.assertIn("time.sleep(20)", publish)
        self.assertIn("HEAD_SHA: ${{ inputs.headSHA }}", publish)
        self.assertIn("- e2e-selection\n", publish)
        self.assertNotIn("- e2e-executor-result", publish)
        self.assertIn(
            'requiredJobIds = ["e2e-selection", "e2e-control-validation"] + selectedJobIds',
            resultBlock,
        )
        self.assertIn("required x86 E2E Jobs did not succeed", resultBlock)
        self.assertNotIn("netpol-path-filter", blocks)
        resultNeeds = set(re.findall(r"(?m)^      - ([a-z0-9-]+)$", resultBlock))
        self.assertEqual(
            resultNeeds,
            testJobs | {"e2e-selection", "e2e-control-validation"},
        )
        pushNeeds = set(re.findall(r"(?m)^      - ([a-z0-9-]+)$", blocks["push"]))
        self.assertEqual(pushNeeds, {"e2e-executor-result"})
        for jobId in testJobs:
            with self.subTest(jobId=jobId):
                self.assertIn(f"      - {jobId}\n", resultBlock)
                block = blocks[jobId]
                self.assertRegex(block, r"(?m)^    needs:(?:\n      - .+)+\n")
                self.assertIn("e2e-selection", block)
                self.assertNotIn("e2e-control-validation", block)
                self.assertIn(
                    f"contains(fromJSON(needs.e2e-selection.outputs.executionJobIds), '{jobId}')",
                    block,
                )
                self.assertNotIn("github.event_name != 'workflow_dispatch'", block)
                self.assertIn("ref: ${{ env.EXECUTION_SHA }}", block)
                self.assertIn("persist-credentials: false", block)
        self.assertIn("github.event_name == 'push'", blocks["push"])
        self.assertIn("needs.e2e-executor-result.result == 'success'", blocks["push"])
        self.assertNotIn("github.event_name != 'workflow_dispatch'", blocks["push"])

    def testKindPullUsesAnonymousGhcrWhenTokenIsAbsent(self):
        makefile = (repoRoot / "makefiles/kind.mk").read_text()

        self.assertIn('if [ -n "$${GHCR_TOKEN:-}" ]', makefile)
        self.assertNotIn(
            "@echo $${GHCR_TOKEN} | docker login ghcr.io",
            makefile,
        )


if __name__ == "__main__":
    unittest.main()
