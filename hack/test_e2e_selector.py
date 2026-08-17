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

import e2e_selector as e2eSelector


class E2ESelectorTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.catalog = e2eSelector.loadCatalog(repoRoot / ".github/e2e-selection.json")

    def select(self, paths, labels=(), requestedGroups=()):
        return e2eSelector.select(
            self.catalog,
            paths,
            labels,
            requestedGroups,
            "0123456789abcdef",
        )

    def testCatalogCoversCurrentX86TestJobs(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        e2eSelector.validateWorkflow(self.catalog, workflow)

    def testCatalogRejectsSameSizeWorkflowMatrixDrift(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        drifted = workflow.replace(
            """        ip-family:
          - ipv4
          - ipv6
          - dual
        mode:""",
            """        ip-family:
          - ipv4
          - ipv6
          - bogus
        mode:""",
            1,
        )
        self.assertNotEqual(workflow, drifted)

        with self.assertRaisesRegex(ValueError, "catalog matrix does not match"):
            e2eSelector.validateWorkflow(self.catalog, drifted)

    def testCatalogRejectsDuplicateWorkflowMatrixEntries(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        drifted = workflow.replace(
            """          - ipv6
          - dual
        mode:""",
            """          - ipv6
          - dual
          - dual
        mode:""",
            1,
        )
        self.assertNotEqual(workflow, drifted)

        with self.assertRaisesRegex(ValueError, "catalog matrix does not match"):
            e2eSelector.validateWorkflow(self.catalog, drifted)

    def testSmokeIsAlwaysSelected(self):
        plan = self.select(["docs/design.md"])

        self.assertFalse(plan["full"])
        self.assertEqual(plan["selectedGroups"], [])
        self.assertEqual(len(plan["matrix"]), 3)
        self.assertEqual({entry["selection"] for entry in plan["matrix"]}, {"smoke"})
        self.assertEqual(
            [
                {key: value for key, value in entry.items() if key != "selection"}
                for entry in plan["matrix"]
            ],
            [
                {"job": "kube-ovn-conformance-e2e", "ip-family": "ipv4", "mode": "overlay"},
                {"job": "kube-ovn-conformance-e2e", "ip-family": "ipv4", "mode": "underlay"},
                {"job": "k8s-conformance-e2e", "ip-family": "ipv4", "mode": "overlay"},
            ],
        )

    def testSmokeMatricesContainOnlyTheThreeApprovedRunnerJobs(self):
        plan = self.select(["docs/design.md"])

        self.assertEqual(
            e2eSelector.jobMatrix(plan, "k8s-conformance-e2e"),
            {"include": [{"ip-family": "ipv4", "mode": "overlay"}]},
        )
        self.assertEqual(
            e2eSelector.jobMatrix(plan, "kube-ovn-conformance-e2e"),
            {
                "include": [
                    {"ip-family": "ipv4", "mode": "overlay"},
                    {"ip-family": "ipv4", "mode": "underlay"},
                ]
            },
        )

    def testCoreSelectionRestoresTheFullConformanceMatrices(self):
        plan = self.select(["docs/design.md"], requestedGroups=["core"])

        self.assertEqual(
            len(e2eSelector.jobMatrix(plan, "k8s-conformance-e2e")["include"]),
            6,
        )
        self.assertEqual(
            len(e2eSelector.jobMatrix(plan, "kube-ovn-conformance-e2e")["include"]),
            6,
        )

    def testExplicitForceFullReasonOverridesAVisibleSafeFileList(self):
        plan = e2eSelector.select(
            self.catalog,
            ["docs/design.md"],
            [],
            [],
            "0123456789abcdef",
            "pull request file list is incomplete; the full suite is required",
        )

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 82)
        self.assertIn("file list is incomplete", plan["fullReason"])

    def testPathsLabelsAndRequestsAreUnioned(self):
        plan = self.select(
            ["test/e2e/cnp-domain/e2e_test.go"],
            labels=["e2e:multi-cni"],
            requestedGroups=["nat-egress"],
        )

        self.assertFalse(plan["full"])
        self.assertEqual(
            plan["selectedGroups"],
            ["multi-cni", "nat-egress", "policy"],
        )
        self.assertEqual(plan["automaticGroups"], [])
        self.assertEqual(plan["recommendedGroups"], ["multi-cni", "policy"])
        self.assertEqual(plan["requestedGroups"], ["nat-egress"])
        self.assertTrue(plan["approvalRequired"])
        self.assertEqual(len(plan["matrix"]), 3 + 6 + 5 + 15)
        self.assertTrue(any(reason["source"] == "path" for reason in plan["reasons"]))
        self.assertTrue(any(reason["source"] == "label" for reason in plan["reasons"]))
        self.assertTrue(any(reason["source"] == "request" for reason in plan["reasons"]))

    def testThreePathGroupsPromoteToFull(self):
        plan = self.select(
            [
                "test/e2e/cnp-domain/e2e_test.go",
                "test/e2e/multus/e2e_test.go",
                "test/e2e/vpc-egress-gateway/e2e_test.go",
            ]
        )

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 82)
        self.assertIn("matched 3 test groups", plan["fullReason"])

    def testUnknownProductionPathPromotesToFull(self):
        plan = self.select(["pkg/new-component/new_feature.go"])

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 82)
        self.assertIn("unclassified production path", plan["fullReason"])

    def testCommonPathPromotesToFull(self):
        plan = self.select(["pkg/apis/kubeovn/v1/types.go"])

        self.assertTrue(plan["full"])
        self.assertIn("shared path", plan["fullReason"])
        self.assertEqual(plan["automaticGroups"], sorted(self.catalog["groups"]))
        self.assertEqual(plan["recommendedGroups"], [])
        self.assertFalse(plan["approvalRequired"])

    def testControllerPathPromotesToFull(self):
        for path in [
            "pkg/controller/network_policy.go",
            "pkg/controller/service.go",
            "pkg/controller/ovn_ic_controller.go",
            "pkg/controller/vpc_nat_gateway.go",
            "pkg/controller/ipsec.go",
        ]:
            with self.subTest(path=path):
                plan = self.select([path])
                self.assertTrue(plan["full"])
                self.assertEqual(len(plan["matrix"]), 82)
                self.assertIn("shared path", plan["fullReason"])

    def testInstallationBuildAndCodegenPathsPromoteToFull(self):
        for path in [
            "VERSION",
            "charts/kube-ovn/Chart.yaml",
            "charts/kube-ovn-v2/Chart.yaml",
            "makefiles/ut.mk",
            "hack/gen-crd.sh",
            "hack/update-codegen.sh",
        ]:
            with self.subTest(path=path):
                plan = self.select([path])
                self.assertTrue(plan["full"])
                self.assertEqual(len(plan["matrix"]), 82)
                self.assertIn("shared path", plan["fullReason"])

    def testSharedE2ESourcesSelectEveryExecutingGroup(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        makefile = (repoRoot / "makefiles/e2e.mk").read_text()
        targetMatches = list(
            re.finditer(r"^([a-z0-9][a-z0-9-]+):[^\n]*\n", makefile, re.MULTILINE)
        )
        targetBodies = {
            match.group(1): makefile[
                match.end() : targetMatches[index + 1].start()
                if index + 1 < len(targetMatches)
                else None
            ]
            for index, match in enumerate(targetMatches)
        }
        workflowBlocks = e2eSelector.workflowJobBlocks(workflow)
        expectedGroupsByDirectory = {}
        for groupName, group in self.catalog["groups"].items():
            for job in group["jobs"]:
                targets = set(re.findall(r"\bmake\s+([a-z0-9][a-z0-9-]+)", workflowBlocks[job["id"]]))
                directories = {
                    directory
                    for target in targets
                    for directory in re.findall(
                        r"\./test/e2e/([a-z0-9-]+)", targetBodies.get(target, "")
                    )
                }
                for directory in directories:
                    expectedGroupsByDirectory.setdefault(directory, set()).add(groupName)

        self.assertGreater(len(expectedGroupsByDirectory), 10)
        for directory, expectedGroups in expectedGroupsByDirectory.items():
            path = f"test/e2e/{directory}/e2e_test.go"
            with self.subTest(path=path):
                plan = self.select([path])
                mappedGroups = {
                    group
                    for reason in plan["reasons"]
                    if reason["source"] == "path"
                    for group in reason["groups"]
                }
                if plan["full"]:
                    self.assertTrue(expectedGroups)
                else:
                    self.assertEqual(mappedGroups, expectedGroups)
                if len(expectedGroups) >= self.catalog["fullThreshold"]:
                    self.assertTrue(plan["full"])

    def testInfrastructureWorkflowScriptsPromoteToFull(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        blocks = e2eSelector.workflowJobBlocks(workflow)
        scripts = {
            script
            for jobId in e2eSelector.infrastructureJobs
            for script in re.findall(r"(?:\./)?(hack/[a-z0-9_./-]+\.sh)", blocks.get(jobId, ""))
            if (repoRoot / script).is_file()
        }
        self.assertIn("hack/go-list.sh", scripts)

        for script in scripts:
            with self.subTest(script=script):
                plan = self.select([script])
                self.assertTrue(plan["full"])
                self.assertEqual(len(plan["matrix"]), 82)

    def testSharedE2EMakeScriptsPromoteToFull(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        makefile = (repoRoot / "Makefile").read_text()
        blocks = e2eSelector.workflowJobBlocks(workflow)
        targetCounts = {}
        for jobId in e2eSelector.workflowTestJobs(workflow):
            for target in set(re.findall(r"\bmake\s+([a-z0-9][a-z0-9-]+)", blocks[jobId])):
                targetCounts[target] = targetCounts.get(target, 0) + 1
        sharedTargets = {target for target, count in targetCounts.items() if count >= 3}
        scripts = {
            script
            for target in sharedTargets
            for script in re.findall(
                rf"(?ms)^{re.escape(target)}:[^\n]*\n(.*?)(?=^[a-z0-9][a-z0-9-]+:|\Z)",
                makefile,
            )
            for script in re.findall(r"(?:\./)?(hack/[a-z0-9_./-]+\.sh)", script)
            if (repoRoot / script).is_file()
        }
        self.assertIn("hack/ci-check-crash.sh", scripts)

        for script in scripts:
            with self.subTest(script=script):
                plan = self.select([script])
                self.assertTrue(plan["full"])
                self.assertEqual(len(plan["matrix"]), 82)

    def testForceFullLabelPromotesToFull(self):
        plan = self.select(["docs/design.md"], labels=["e2e:full"])

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 82)
        self.assertEqual(plan["fullReason"], "label e2e:full requested the full suite")

    def testInvalidRequestedGroupFails(self):
        with self.assertRaisesRegex(ValueError, "unknown requested group"):
            self.select(["docs/design.md"], requestedGroups=["not-a-group"])

    def testRequestedGroupsJsonFeedsTheCli(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            paths = directory / "paths"
            plan = directory / "plan.json"
            paths.write_bytes(b"docs/design.md\0")

            subprocess.run(
                [
                    sys.executable,
                    str(repoRoot / "hack/e2e_selector.py"),
                    "--paths-file",
                    str(paths),
                    "--request-groups-json",
                    '["policy", "multi-cni"]',
                    "--head-sha",
                    "0123456789abcdef",
                    "--plan-file",
                    str(plan),
                ],
                cwd=repoRoot,
                check=True,
            )

            result = json.loads(plan.read_text())
            self.assertEqual(result["requestedGroups"], ["multi-cni", "policy"])

    def testInvalidStructuredInputFallsBackToFullThroughCLI(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            paths = directory / "paths"
            paths.write_bytes(b"docs/design.md\0")
            for name, option in {
                "request": ("--request-group", "not-a-group"),
                "label": ("--label", "e2e:polcy"),
            }.items():
                with self.subTest(name=name):
                    plan = directory / f"{name}-plan.json"
                    subprocess.run(
                        [
                            sys.executable,
                            str(repoRoot / "hack/e2e_selector.py"),
                            "--paths-file",
                            str(paths),
                            *option,
                            "--head-sha",
                            "0123456789abcdef",
                            "--plan-file",
                            str(plan),
                        ],
                        cwd=repoRoot,
                        check=True,
                    )
                    result = json.loads(plan.read_text())
                    self.assertTrue(result["full"])
                    self.assertEqual(len(result["matrix"]), 82)
                    self.assertIn("selection error", result["fullReason"])

    def testPlanIsJsonSerializableAndBoundToHead(self):
        plan = self.select(["test/e2e/ipsec/e2e_test.go"])

        self.assertEqual(plan["headSHA"], "0123456789abcdef")
        self.assertEqual(plan["schemaVersion"], 1)
        self.assertIn("catalogRevision", plan)
        json.dumps(plan)

    def testNulDelimitedPathsPreserveNewlines(self):
        pathFile = repoRoot / "e2e-selector-paths.test"
        try:
            pathFile.write_bytes(b"docs/normal.md\0test/e2e/cnp-domain/file\nname.go\0")
            self.assertEqual(
                e2eSelector.readPaths(pathFile),
                ["docs/normal.md", "test/e2e/cnp-domain/file\nname.go"],
            )
        finally:
            pathFile.unlink(missing_ok=True)

    def testRenameRecordsPreserveOldAndNewPaths(self):
        paths = e2eSelector.changedPathsFromNameStatus(
            b"R100\0pkg/controller/x.go\0docs/x.go\0"
            b"R085\0.github/workflows/x.yml\0misc/x.yml\0"
            b"R090\0test/e2e/k8s-network/e2e_test.go\0test/e2e/multus/e2e_test.go\0"
        )

        self.assertEqual(
            paths,
            [
                "pkg/controller/x.go",
                "docs/x.go",
                ".github/workflows/x.yml",
                "misc/x.yml",
                "test/e2e/k8s-network/e2e_test.go",
                "test/e2e/multus/e2e_test.go",
            ],
        )

    def testNameStatusPreservesTabsAndNewlines(self):
        paths = e2eSelector.changedPathsFromNameStatus(
            b"M\0test/e2e/cnp-domain/file\nname.go\0A\0docs/tab\tname.md\0"
        )

        self.assertEqual(
            paths,
            ["test/e2e/cnp-domain/file\nname.go", "docs/tab\tname.md"],
        )

    def testSummaryEscapesUntrustedPathText(self):
        path = "pkg/![loaded](https://example.invalid/pixel)`code`<details>\n## injected.go"
        plan = self.select([path])

        summary = e2eSelector.renderSummary(plan, [path])
        self.assertNotIn("\n## injected", summary)
        self.assertNotIn("<details>", summary)
        self.assertNotIn("![loaded]", summary)
        self.assertNotIn("](", summary)
        self.assertNotIn("`code`", summary)
        self.assertIn("&#60;details&#62; &#35;&#35; injected&#46;go", summary)

    def testSummaryShowsAutomaticAndDeferredCoverage(self):
        plan = self.select(["test/e2e/cnp-domain/e2e_test.go"])

        summary = e2eSelector.renderSummary(plan, ["test/e2e/cnp-domain/e2e_test.go"])
        self.assertIn("Automatic coverage: mandatory smoke &#40;3 runner jobs&#41;", summary)
        self.assertIn("Recommended deferred groups: policy", summary)
        self.assertIn("Approval required: `yes`", summary)
        self.assertIn("Waiting for: `/test e2e`", summary)
        self.assertIn("Alternative: `/test e2e policy`", summary)

    def testCatalogFailureEmitsFullFallbackPlan(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            paths = directory / "paths"
            paths.write_bytes(b"pkg/controller/pod.go\0")
            malformed = directory / "malformed.json"
            malformed.write_text("{")
            incompatible = directory / "incompatible.json"
            incompatible.write_text('{"schemaVersion": 999}')
            invalidStructure = directory / "invalid-structure.json"
            invalidStructure.write_text('{"schemaVersion": 1, "groups": {"core": null}}')
            noSmoke = directory / "no-smoke.json"
            noSmoke.write_text(
                json.dumps({**self.catalog, "smoke": []})
            )

            for name, catalog in {
                "missing": directory / "missing.json",
                "malformed": malformed,
                "incompatible": incompatible,
                "invalid-structure": invalidStructure,
                "no-smoke": noSmoke,
            }.items():
                with self.subTest(name=name):
                    plan = directory / f"{name}-plan.json"
                    summary = directory / f"{name}-summary.md"
                    subprocess.run(
                        [
                            sys.executable,
                            str(repoRoot / "hack/e2e_selector.py"),
                            "--catalog",
                            str(catalog),
                            "--paths-file",
                            str(paths),
                            "--head-sha",
                            "0123456789abcdef",
                            "--plan-file",
                            str(plan),
                            "--summary-file",
                            str(summary),
                        ],
                        cwd=repoRoot,
                        check=True,
                    )

                    result = json.loads(plan.read_text())
                    self.assertTrue(result["full"])
                    self.assertEqual(result["headSHA"], "0123456789abcdef")
                    self.assertEqual(len(result["matrix"]), 82)
                    self.assertIn("catalog error", result["fullReason"])
                    self.assertIn("Full suite: `yes`", summary.read_text())

    def testCatalogFailureFallbackAcceptsInlineWorkflowMatrix(self):
        workflow = (repoRoot / ".github/workflows/build-x86-image.yaml").read_text()
        inlineWorkflow = workflow.replace(
            """        ip-family:
          - ipv4
          - ipv6
          - dual
        mode:""",
            """        ip-family: [ipv4, ipv6, dual]
        mode:""",
            1,
        )
        self.assertNotEqual(workflow, inlineWorkflow)

        plan = e2eSelector.fallbackPlan(
            "0123456789abcdef",
            "missing catalog",
            inlineWorkflow,
            "catalog",
        )
        self.assertEqual(len(plan["matrix"]), 82)

    def testInvalidEventPayloadFallsBackToFullThroughCLI(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            paths = directory / "paths"
            paths.write_bytes(b"docs/design.md\0")
            malformed = directory / "malformed.json"
            malformed.write_text("{")
            invalidLabel = directory / "invalid-label.json"
            invalidLabel.write_text('{"pull_request": {"labels": [{}]}}')
            missingPullRequest = directory / "missing-pull-request.json"
            missingPullRequest.write_text("{}")
            missingLabels = directory / "missing-labels.json"
            missingLabels.write_text('{"pull_request": {}}')

            for name, event in {
                "missing": directory / "missing.json",
                "malformed": malformed,
                "invalid-label": invalidLabel,
                "missing-pull-request": missingPullRequest,
                "missing-labels": missingLabels,
            }.items():
                with self.subTest(name=name):
                    plan = directory / f"{name}-plan.json"
                    subprocess.run(
                        [
                            sys.executable,
                            str(repoRoot / "hack/e2e_selector.py"),
                            "--paths-file",
                            str(paths),
                            "--event-file",
                            str(event),
                            "--head-sha",
                            "0123456789abcdef",
                            "--plan-file",
                            str(plan),
                        ],
                        cwd=repoRoot,
                        check=True,
                    )
                    result = json.loads(plan.read_text())
                    self.assertTrue(result["full"])
                    self.assertEqual(len(result["matrix"]), 82)
                    self.assertIn("selection error", result["fullReason"])

    def testSelectorFilesHaveCodeOwners(self):
        codeowners = (repoRoot / ".github/CODEOWNERS").read_text()

        for path in [
            "/.github/e2e-selection.json",
            "/.github/workflows/build-x86-image.yaml",
            "/.github/workflows/x86-e2e-dispatcher.yaml",
            "/.github/workflows/x86-e2e-gate.yaml",
            "/hack/e2e_control.py",
            "/hack/e2e_selector.py",
            "/hack/test_e2e_control.py",
            "/hack/test_e2e_selector.py",
        ]:
            with self.subTest(path=path):
                self.assertRegex(
                    codeowners,
                    rf"(?m)^{re.escape(path)}\s+@oilbeater\s+@zhangzujian$",
                )

    def testEveryPathMappingHasARegressionCase(self):
        cases = [
            ("test/e2e/kube-ovn/subnet/subnet.go", "core"),
            ("test/e2e/k8s-network/e2e_test.go", "policy"),
            ("test/e2e/connectivity/e2e_test.go", "core"),
            ("test/e2e/cnp-domain/e2e_test.go", "policy"),
            ("pkg/controller/network_policy.go", "policy"),
            ("test/e2e/bgp/e2e_test.go", "bgp-routing"),
            ("test/e2e/multus/e2e_test.go", "multi-cni"),
            ("test/e2e/lb-svc/e2e_test.go", "service-lb-underlay"),
            ("pkg/controller/service.go", "service-lb-underlay"),
            ("charts/kube-ovn/Chart.yaml", "install-platform"),
            ("test/e2e/ha/e2e_test.go", "ha-hosted"),
            ("test/e2e/ovn-ic/e2e_test.go", "multi-cluster"),
            ("pkg/controller/ovn_ic_controller.go", "multi-cluster"),
            ("test/e2e/vpc-egress-gateway/e2e_test.go", "nat-egress"),
            ("pkg/controller/vpc_egress_gateway.go", "nat-egress"),
            ("test/e2e/webhook/e2e_test.go", "security-webhook"),
            ("test/e2e/security/e2e_test.go", "ha-hosted"),
            ("pkg/webhook/webhook.go", "security-webhook"),
        ]
        self.assertEqual(len(cases), len(self.catalog["pathRules"]))

        for path, expectedGroup in cases:
            with self.subTest(path=path):
                plan = self.select([path])
                mappedGroups = {
                    group
                    for reason in plan["reasons"]
                    if reason["source"] == "path"
                    for group in reason["groups"]
                }
                self.assertIn(expectedGroup, mappedGroups)

    def testEveryBuiltE2ESourceIsClassified(self):
        makefile = (repoRoot / "makefiles/e2e.mk").read_text()
        sourceDirectories = sorted(
            set(re.findall(r"\./test/e2e/([a-z0-9-]+)", makefile))
        )
        self.assertTrue(sourceDirectories)

        for directory in sourceDirectories:
            path = f"test/e2e/{directory}/e2e_test.go"
            with self.subTest(path=path):
                plan = self.select([path])
                self.assertTrue(
                    plan["full"] or plan["selectedGroups"],
                    f"{path} silently selected smoke only",
                )

    def testSharedE2EFrameworkPromotesToFull(self):
        plan = self.select(["test/e2e/framework/pod.go"])

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 82)


if __name__ == "__main__":
    unittest.main()
