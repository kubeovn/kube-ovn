#!/usr/bin/env python3

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "hack"))

import e2e_selector as e2eSelector


class E2ESelectorTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.catalog = e2eSelector.loadCatalog(ROOT / ".github/e2e-selection.json")

    def select(self, paths, labels=(), requestedGroups=()):
        return e2eSelector.select(
            self.catalog,
            paths,
            labels,
            requestedGroups,
            "0123456789abcdef",
        )

    def testCatalogCoversCurrentX86TestJobs(self):
        workflow = (ROOT / ".github/workflows/build-x86-image.yaml").read_text()
        e2eSelector.validateWorkflow(self.catalog, workflow)

    def testCatalogRejectsSameSizeWorkflowMatrixDrift(self):
        workflow = (ROOT / ".github/workflows/build-x86-image.yaml").read_text()
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

    def testSmokeIsAlwaysSelected(self):
        plan = self.select(["docs/design.md"])

        self.assertFalse(plan["full"])
        self.assertEqual(plan["selectedGroups"], [])
        self.assertEqual(len(plan["matrix"]), 3)
        self.assertEqual({entry["selection"] for entry in plan["matrix"]}, {"smoke"})

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
        self.assertEqual(len(plan["matrix"]), 81)
        self.assertIn("matched 3 test groups", plan["fullReason"])

    def testUnknownProductionPathPromotesToFull(self):
        plan = self.select(["pkg/controller/new_feature.go"])

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 81)
        self.assertIn("unclassified production path", plan["fullReason"])

    def testCommonPathPromotesToFull(self):
        plan = self.select(["pkg/apis/kubeovn/v1/types.go"])

        self.assertTrue(plan["full"])
        self.assertIn("shared path", plan["fullReason"])

    def testForceFullLabelPromotesToFull(self):
        plan = self.select(["docs/design.md"], labels=["e2e:full"])

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 81)
        self.assertEqual(plan["fullReason"], "label e2e:full requested the full suite")

    def testInvalidRequestedGroupFails(self):
        with self.assertRaisesRegex(ValueError, "unknown requested group"):
            self.select(["docs/design.md"], requestedGroups=["not-a-group"])

    def testInvalidRequestedGroupFailsThroughCLI(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            paths = directory / "paths"
            paths.write_bytes(b"docs/design.md\0")
            result = subprocess.run(
                [
                    sys.executable,
                    str(ROOT / "hack/e2e_selector.py"),
                    "--paths-file",
                    str(paths),
                    "--request-group",
                    "not-a-group",
                    "--head-sha",
                    "0123456789abcdef",
                    "--plan-file",
                    str(directory / "plan.json"),
                ],
                cwd=ROOT,
                capture_output=True,
                text=True,
            )

            self.assertNotEqual(result.returncode, 0)
            self.assertIn("unknown requested group", result.stderr)

    def testPlanIsJsonSerializableAndBoundToHead(self):
        plan = self.select(["test/e2e/ipsec/e2e_test.go"])

        self.assertEqual(plan["headSHA"], "0123456789abcdef")
        self.assertEqual(plan["schemaVersion"], 1)
        self.assertIn("catalogRevision", plan)
        json.dumps(plan)

    def testNulDelimitedPathsPreserveNewlines(self):
        pathFile = ROOT / "e2e-selector-paths.test"
        try:
            pathFile.write_bytes(b"docs/normal.md\0test/e2e/cnp-domain/file\nname.go\0")
            self.assertEqual(
                e2eSelector.readPaths(pathFile),
                ["docs/normal.md", "test/e2e/cnp-domain/file\nname.go"],
            )
        finally:
            pathFile.unlink(missing_ok=True)

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

    def testCatalogFailureEmitsFullFallbackPlan(self):
        with tempfile.TemporaryDirectory() as directory:
            directory = Path(directory)
            paths = directory / "paths"
            paths.write_bytes(b"pkg/controller/pod.go\0")
            malformed = directory / "malformed.json"
            malformed.write_text("{")
            incompatible = directory / "incompatible.json"
            incompatible.write_text('{"schemaVersion": 999}')

            for name, catalog in {
                "missing": directory / "missing.json",
                "malformed": malformed,
                "incompatible": incompatible,
            }.items():
                with self.subTest(name=name):
                    plan = directory / f"{name}-plan.json"
                    summary = directory / f"{name}-summary.md"
                    subprocess.run(
                        [
                            sys.executable,
                            str(ROOT / "hack/e2e_selector.py"),
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
                        cwd=ROOT,
                        check=True,
                    )

                    result = json.loads(plan.read_text())
                    self.assertTrue(result["full"])
                    self.assertEqual(result["headSHA"], "0123456789abcdef")
                    self.assertEqual(len(result["matrix"]), 81)
                    self.assertIn("catalog error", result["fullReason"])
                    self.assertIn("Full suite: `yes`", summary.read_text())

    def testEveryPathMappingHasARegressionCase(self):
        cases = [
            ("test/e2e/kube-ovn/subnet/subnet.go", "core"),
            ("test/e2e/cnp-domain/e2e_test.go", "policy"),
            ("pkg/controller/network_policy.go", "policy"),
            ("test/e2e/multus/e2e_test.go", "multi-cni"),
            ("test/e2e/lb-svc/e2e_test.go", "service-lb-underlay"),
            ("pkg/controller/service.go", "service-lb-underlay"),
            ("charts/kube-ovn/Chart.yaml", "install-platform"),
            ("test/e2e/ha/e2e_test.go", "ha-hosted"),
            ("test/e2e/kube-ovn-ic/e2e_test.go", "multi-cluster"),
            ("pkg/controller/ovn_ic_controller.go", "multi-cluster"),
            ("test/e2e/vpc-egress-gateway/e2e_test.go", "nat-egress"),
            ("pkg/controller/vpc_egress_gateway.go", "nat-egress"),
            ("test/e2e/webhook/e2e_test.go", "security-webhook"),
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


if __name__ == "__main__":
    unittest.main()
