#!/usr/bin/env python3

import json
import sys
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / "hack"))

import e2e_selector


class E2ESelectorTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.catalog = e2e_selector.load_catalog(ROOT / ".github/e2e-selection.json")

    def select(self, paths, labels=(), requested_groups=()):
        return e2e_selector.select(
            self.catalog,
            paths,
            labels,
            requested_groups,
            "0123456789abcdef",
        )

    def test_catalog_covers_current_x86_test_jobs(self):
        workflow = (ROOT / ".github/workflows/build-x86-image.yaml").read_text()
        workflow_jobs = e2e_selector.workflow_test_jobs(workflow)
        catalog_jobs = {
            job["id"]
            for group in self.catalog["groups"].values()
            for job in group["jobs"]
        }

        self.assertEqual(workflow_jobs, catalog_jobs)
        self.assertEqual(
            len(e2e_selector.expand_all(self.catalog)),
            self.catalog["expectedRunnerJobs"],
        )

    def test_smoke_is_always_selected(self):
        plan = self.select(["docs/design.md"])

        self.assertFalse(plan["full"])
        self.assertEqual(plan["selectedGroups"], [])
        self.assertEqual(len(plan["matrix"]), 3)
        self.assertEqual({entry["selection"] for entry in plan["matrix"]}, {"smoke"})

    def test_paths_labels_and_requests_are_unioned(self):
        plan = self.select(
            ["test/e2e/cnp-domain/e2e_test.go"],
            labels=["e2e:multi-cni"],
            requested_groups=["nat-egress"],
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

    def test_three_path_groups_promote_to_full(self):
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

    def test_unknown_production_path_promotes_to_full(self):
        plan = self.select(["pkg/controller/new_feature.go"])

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 81)
        self.assertIn("unclassified production path", plan["fullReason"])

    def test_common_path_promotes_to_full(self):
        plan = self.select(["pkg/apis/kubeovn/v1/types.go"])

        self.assertTrue(plan["full"])
        self.assertIn("shared path", plan["fullReason"])

    def test_force_full_label_promotes_to_full(self):
        plan = self.select(["docs/design.md"], labels=["e2e:full"])

        self.assertTrue(plan["full"])
        self.assertEqual(len(plan["matrix"]), 81)
        self.assertEqual(plan["fullReason"], "label e2e:full requested the full suite")

    def test_invalid_requested_group_fails(self):
        with self.assertRaisesRegex(ValueError, "unknown requested group"):
            self.select(["docs/design.md"], requested_groups=["not-a-group"])

    def test_plan_is_json_serializable_and_bound_to_head(self):
        plan = self.select(["test/e2e/ipsec/e2e_test.go"])

        self.assertEqual(plan["headSHA"], "0123456789abcdef")
        self.assertEqual(plan["schemaVersion"], 1)
        self.assertIn("catalogRevision", plan)
        json.dumps(plan)

    def test_nul_delimited_paths_preserve_newlines(self):
        path_file = ROOT / "e2e-selector-paths.test"
        try:
            path_file.write_bytes(b"docs/normal.md\0test/e2e/cnp-domain/file\nname.go\0")
            self.assertEqual(
                e2e_selector.read_paths(path_file),
                ["docs/normal.md", "test/e2e/cnp-domain/file\nname.go"],
            )
        finally:
            path_file.unlink(missing_ok=True)

    def test_summary_escapes_untrusted_path_text(self):
        plan = self.select(["pkg/controller/<details>\n## injected.go"])

        summary = e2e_selector.render_summary(plan, ["pkg/controller/<details>\n## injected.go"])
        self.assertNotIn("\n## injected", summary)
        self.assertNotIn("<details>", summary)
        self.assertIn("&lt;details&gt; ## injected.go", summary)


if __name__ == "__main__":
    unittest.main()
