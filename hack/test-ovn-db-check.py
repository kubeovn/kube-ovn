#!/usr/bin/env python3
"""Exercise startup validation with real OVSDB journals and no live peers."""

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[1]
IMAGES = ROOT / "dist/images"
TOOL = shutil.which("ovsdb-tool")


def record(value):
    body = json.dumps(value, separators=(",", ":")).encode() + b"\n"
    return b"OVSDB CLUSTER %d %s\n" % (
        len(body), hashlib.sha1(body, usedforsecurity=False).hexdigest().encode()
    ) + body


@unittest.skipUnless(TOOL, "ovsdb-tool is required")
class DatabaseCheckTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="ovn-db-check-")
        self.addCleanup(self.temp.cleanup)
        self.directory = Path(self.temp.name)

    def database(self, name="OVN_Southbound", filename="ovnsb_db.db"):
        schema = self.directory / "schema.json"
        schema.write_text(json.dumps({
            "name": name, "version": "1.0.0",
            "tables": {"Test": {"isRoot": True, "columns": {"value": {"type": "integer"}}}},
        }))
        path = self.directory / filename
        subprocess.run([TOOL, "create-cluster", str(path), str(schema), "tcp:192.0.2.1:16644"],
                       check=True, capture_output=True)
        # A newly created journal has no election records yet. Establish a
        # valid term so check-cluster can validate a nonempty member history.
        with path.open("ab") as journal:
            journal.write(record({"term": 1}))
        return path

    def check(self, path):
        return subprocess.run(["python3", str(IMAGES / "ovn-db-check.py"), str(path)],
                              capture_output=True, text=True, timeout=10)

    def test_startup_zero_tail_without_quorum(self):
        for script, kind, name, filename in (
            ("start-db.sh", "nb", "OVN_Northbound", "ovnnb_db.db"),
            ("start-db.sh", "sb", "OVN_Southbound", "ovnsb_db.db"),
            ("start-ic-db.sh", "ic_nb", "OVN_IC_Northbound", "ovn_ic_nb_db.db"),
            ("start-ic-db.sh", "ic_sb", "OVN_IC_Southbound", "ovn_ic_sb_db.db"),
        ):
            with self.subTest(database=kind):
                path = self.database(name, filename)
                original = path.read_bytes() + b"\0" * 80
                path.write_bytes(original)
                # Preserve the valid identity used by the existing recovery path.
                with path.open("rb") as source:
                    length = int(source.readline().split()[2])
                    path.with_suffix(".hdr").write_bytes(source.read(length))
                content = (IMAGES / script).read_text()
                functions = content[content.index("function "):content.index("\ntrap quit EXIT")]
                functions = functions.replace("/etc/ovn", str(self.directory))
                functions = functions.replace("/kube-ovn/", str(IMAGES) + "/")
                command = "set -eo pipefail\n" + functions + """
function get_live_cluster_id { echo unexpected-peer-recovery >&2; return 1; }
function create_local_config { :; }
ovn_db_pre_start "$TEST_DB_KIND"
echo startup-reached
"""
                env = dict(os.environ, NODE_IPS="192.0.2.1,192.0.2.2,192.0.2.3",
                           POD_IP="192.0.2.1", DB_CLUSTER_ADDR="192.0.2.1", ENABLE_SSL="false",
                           NB_CLUSTER_PORT="6643", SB_CLUSTER_PORT="6644", TEST_DB_KIND=kind,
                           UUID_RE="[0-9a-fA-F-]{36}")
                result = subprocess.run(["bash", "-c", command], env=env, capture_output=True,
                                        text=True, timeout=10)
                self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
                self.assertIn("startup-reached", result.stdout)
                self.assertIn("validated Raft prefix", result.stderr)
                self.assertNotIn("unexpected-peer-recovery", result.stderr)
                self.assertEqual(path.read_bytes(), original)
                self.assertEqual(list(self.directory.glob(filename + ".backup-*")), [])

    def test_zero_tail_preserves_original_and_identity(self):
        path = self.database()
        valid = path.read_bytes()
        original_files = sorted(p.name for p in self.directory.iterdir())
        for size in (1, 80, 1024 * 1024):
            with self.subTest(zero_bytes=size):
                original = valid + b"\0" * size
                path.write_bytes(original)
                result = self.check(path)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(path.read_bytes(), original)
                self.assertEqual(sorted(p.name for p in self.directory.iterdir()),
                                 original_files)

    def test_probes_accept_zero_tail_but_reject_semantic_gap(self):
        appctl = self.directory / "ovn-appctl"
        appctl.write_text("#!/bin/bash\necho runtime-reached >> \"$COMMAND_LOG\"\necho 'Status: cluster member'\necho 'Role: follower'\n")
        appctl.chmod(0o700)
        ctl = self.directory / "ovn-ctl"
        ctl.write_text("#!/bin/bash\necho running\n")
        ctl.chmod(0o700)
        trace = self.directory / "commands"
        env = dict(os.environ, PATH=str(self.directory) + ":" + os.environ["PATH"], COMMAND_LOG=str(trace))
        for script, names in (
            ("ovn-healthcheck.sh", (("OVN_Northbound", "ovnnb_db.db"), ("OVN_Southbound", "ovnsb_db.db"))),
            ("ovn-ic-healthcheck.sh", (("OVN_IC_Northbound", "ovn_ic_nb_db.db"), ("OVN_IC_Southbound", "ovn_ic_sb_db.db"))),
        ):
            paths = [self.database(name, filename) for name, filename in names]
            originals = [path.read_bytes() for path in paths]
            for invalid in (None, 0, 1):
                with self.subTest(probe=script, invalid_database=invalid):
                    trace.write_text("")
                    for index, path in enumerate(paths):
                        path.write_bytes(originals[index] + (record({"commit_index": 999}) if index == invalid else b"") + b"\0" * 80)
                    content = (IMAGES / script).read_text().replace("/etc/ovn", str(self.directory))
                    content = content.replace("/kube-ovn/", str(IMAGES) + "/")
                    content = content.replace("/usr/share/ovn/scripts/ovn-ctl", str(ctl))
                    result = subprocess.run(["bash", "-c", content], env=env, capture_output=True,
                                            text=True, timeout=10)
                    self.assertEqual(result.returncode == 0, invalid is None, result.stdout + result.stderr)
                    self.assertEqual("runtime-reached" in trace.read_text(), invalid is None)

    def test_replaced_source_is_not_validated_as_current(self):
        path = self.database()
        original = path.read_bytes() + b"\0" * 80
        path.write_bytes(original)
        spec = importlib.util.spec_from_file_location("ovn_db_check", IMAGES / "ovn-db-check.py")
        helper = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(helper)
        run = subprocess.run

        def replace_after_check(*args, **kwargs):
            result = run(*args, **kwargs)
            replacement = path.with_suffix(".replacement")
            replacement.write_bytes(original)
            replacement.replace(path)
            return result

        with mock.patch.object(helper.subprocess, "run", side_effect=replace_after_check):
            self.assertFalse(helper.validate_zero_tail(path))

    def test_valid_database(self):
        path = self.database()
        original = path.read_bytes()
        result = self.check(path)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(path.read_bytes(), original)

    def test_semantic_gap_with_zero_tail_is_rejected(self):
        path = self.database()
        original = path.read_bytes() + record({"commit_index": 999}) + b"\0" * 80
        path.write_bytes(original)
        result = self.check(path)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("commit", result.stderr)
        self.assertEqual(path.read_bytes(), original)

    def test_other_corruption_is_rejected(self):
        path = self.database()
        valid = path.read_bytes()
        for label, data in (
            ("empty", b""),
            ("zero-header", b"\0" * 80),
            ("middle-zero", valid + b"\0" * 80 + record({"term": 2})),
            ("bad-checksum", valid + record({"term": 2}).replace(b'"term":2', b'"term":3') + b"\0" * 80),
            ("garbage-tail", valid + b"not-a-record\n"),
            ("partial-header", valid + b"OVSDB CLUSTER 80"),
            ("partial-body", valid + record({"term": 2})[:-2]),
        ):
            with self.subTest(corruption=label):
                path.write_bytes(data)
                result = self.check(path)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual(path.read_bytes(), data)


if __name__ == "__main__":
    unittest.main()
