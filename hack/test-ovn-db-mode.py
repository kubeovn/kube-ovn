#!/usr/bin/env python3
"""Exercise the real central startup branches with sandboxed process stubs."""

import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]


class DatabaseModeTest(unittest.TestCase):
    def run_healthcheck(self, addresses, databases):
        with tempfile.TemporaryDirectory(prefix="ovn-healthcheck-") as tmp:
            root = Path(tmp)
            db_dir = root / "db"
            run_dir = root / "run"
            bin_dir = root / "bin"
            for directory in (db_dir, run_dir, bin_dir):
                directory.mkdir()
            for name, content in databases.items():
                (db_dir / name).write_text(content)
            log = root / "commands"
            log.touch()
            stub = '''#!/usr/bin/env bash
set -euo pipefail
name=${0##*/}
printf '%s %s\\n' "$name" "$*" >> "$COMMAND_LOG"
case "$name" in
    ovn-ctl)
        case "$1" in
            status_northd) ;;
            status_ovnnb|status_ovnsb) echo running ;;
        esac ;;
    ovsdb-tool)
        case "$1" in
            db-is-standalone) [[ $(cat "$2") == standalone ]] ;;
            db-is-clustered) [[ $(cat "$2") == clustered ]] ;;
        esac ;;
    ovn-appctl)
        if [[ "$*" == *cluster/status* ]]; then
            echo 'Status: cluster'
            echo 'Role: leader'
        else
            echo 'status: ok'
        fi ;;
esac
'''
            for name in ("ovn-ctl", "ovsdb-tool", "ovn-appctl"):
                executable = bin_dir / name
                executable.write_text(stub)
                executable.chmod(0o755)
            script = (ROOT / "dist/images" / "ovn-healthcheck.sh").read_text()
            replacements = {
                "/usr/share/ovn/scripts/ovn-ctl": f"{bin_dir}/ovn-ctl",
                "/etc/ovn": str(db_dir),
                "/var/run/ovn": str(run_dir),
            }
            for old, new in replacements.items():
                script = script.replace(old, new)
            healthcheck = root / "ovn-healthcheck.sh"
            healthcheck.write_text(script)
            env = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}",
                       COMMAND_LOG=str(log), NODE_IPS=addresses)
            result = subprocess.run(["bash", str(healthcheck)], env=env,
                                    capture_output=True, text=True, timeout=10)
            return result, log.read_text()

    def run_start(self, addresses, ssl=False, databases=None):
        with tempfile.TemporaryDirectory(prefix="ovn-db-mode-") as tmp:
            root = Path(tmp)
            db_dir = root / "db"
            run_dir = root / "run"
            bin_dir = root / "bin"
            for directory in (db_dir, run_dir, bin_dir):
                directory.mkdir()
            for name, content in (databases or {}).items():
                (db_dir / name).write_text(content)
            log = root / "commands"
            log.touch()
            stub = '''#!/usr/bin/env bash
set -euo pipefail
name=${0##*/}
printf '%s %s\\n' "$name" "$*" >> "$COMMAND_LOG"
case "$name" in
    ovn-ctl)
        if [[ "$*" == *restart_northd* ]]; then
            for db in ovnnb_db.db ovnsb_db.db; do
                [[ -e "$TEST_DB_DIR/$db" ]] || echo standalone > "$TEST_DB_DIR/$db"
            done
        fi
        if [[ "$*" == stop_northd && "${STOP_BEFORE_RECOVERY:-}" == true ]]; then
            exit 75
        fi ;;
    ovsdb-tool)
        case "$1" in
            db-is-clustered) [[ $(cat "$2") == clustered ]] ;;
            db-is-standalone) [[ $(cat "$2") == standalone ]] ;;
            db-name) [[ "$2" == *ovnnb* ]] && echo OVN_Northbound || echo OVN_Southbound ;;
        esac ;;
    getent) printf '%s %s.cluster.local\\n' 192.0.2.1 "$3" ;;
esac
'''
            for name in ("ovn-ctl", "ovsdb-tool", "ovsdb-client", "ovn-nbctl", "ovn-sbctl", "ovn-appctl", "kube-ovn-leader-checker", "getent", "kube-ovn-tls-reload.sh"):
                executable = bin_dir / name
                executable.write_text(stub)
                executable.chmod(0o755)
            (bin_dir / "ovs-lib").touch()
            for name in ("ovn-db-mode.sh", "ovn-db-ssl-options.sh"):
                (bin_dir / name).write_text((ROOT / "dist/images" / name).read_text())
            script = (ROOT / "dist/images/start-db.sh").read_text()
            replacements = {
                "/kube-ovn/": f"{bin_dir}/",
                "/usr/share/ovn/scripts/ovn-ctl": f"{bin_dir}/ovn-ctl",
                "/usr/share/openvswitch/scripts/ovs-lib": f"{bin_dir}/ovs-lib",
                "/etc/ovn": str(db_dir),
                "/var/run/ovn": str(run_dir),
            }
            for old, new in replacements.items():
                script = script.replace(old, new)
            startup = root / "start-db.sh"
            startup.write_text(script)
            env = dict(os.environ, PATH=f"{bin_dir}:{os.environ['PATH']}",
                       COMMAND_LOG=str(log), TEST_DB_DIR=str(db_dir), NODE_IPS=addresses,
                       POD_IP=addresses.split(",")[0] or "192.0.2.1", POD_IPS="192.0.2.1",
                       OVN_LEADER_PROBE_INTERVAL="5", ENABLE_SSL=str(ssl).lower(),
                       DEBUG_WRAPPER="valgrind",
                       STOP_BEFORE_RECOVERY=str(any(v in ("clustered", "raft-header") for v in (databases or {}).values())).lower())
            result = subprocess.run(["bash", str(startup)], env=env,
                                    capture_output=True, text=True, timeout=10)
            after = {p.name: p.read_text() for p in db_dir.iterdir()}
            return result, log.read_text(), after

    def test_new_single_instance_uses_standalone(self):
        for address in ("", "192.0.2.1", "fd00::1", "ovn-central-0.ovn-central.hcp.svc"):
            for ssl in (False, True):
                with self.subTest(address=address, ssl=ssl):
                    result, commands, _ = self.run_start(address, ssl)
                    self.assertEqual(result.returncode, 0, result.stderr)
                    self.assertIn("restart_northd", commands)
                    self.assertIn("--ovn-northd-wrapper=valgrind", commands)
                    self.assertNotIn("--db-nb-cluster-local-addr", commands)
                    self.assertIn("kube-ovn-leader-checker --probeInterval=5 --enableCompact=false --remoteAddresses=\n", commands)

    def test_multiple_instances_keep_raft(self):
        for ssl in (False, True):
            result, commands, _ = self.run_start("192.0.2.1,192.0.2.2,192.0.2.3", ssl)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("--db-nb-cluster-local-addr", commands)
            self.assertIn("--db-sb-cluster-local-addr", commands)
            self.assertNotIn("restart_northd", commands)

    def test_multiple_replicas_accept_standalone_files(self):
        databases = {"ovnnb_db.db": "standalone\n", "ovnsb_db.db": "standalone\n"}
        result, commands, after = self.run_start("192.0.2.1,192.0.2.2", databases=databases)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("--db-nb-cluster-local-addr", commands)
        self.assertIn("--db-sb-cluster-local-addr", commands)
        self.assertEqual(after, databases)

    def test_existing_standalone_restarts_without_rewriting_data(self):
        databases = {"ovnnb_db.db": "standalone\n", "ovnsb_db.db": "standalone\n"}
        result, commands, after = self.run_start("192.0.2.1", databases=databases)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("restart_northd", commands)
        self.assertEqual(after, databases)

    def test_existing_raft_retains_cluster_mode(self):
        for databases in ({"ovnnb_db.db": "clustered", "ovnsb_db.db": "clustered"},
                          {"ovnnb_db.hdr": "raft-header"}):
            result, commands, after = self.run_start("192.0.2.1", databases=databases)
            self.assertIn("Preserving existing Raft data", result.stderr)
            self.assertNotIn("restart_northd", commands)
            self.assertEqual(after, databases)

    def test_healthcheck_uses_database_model(self):
        standalone = {"ovnnb_db.db": "standalone\n", "ovnsb_db.db": "standalone\n"}
        result, commands = self.run_healthcheck("192.0.2.1", standalone)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("get-db-storage-status", commands)
        self.assertNotIn("cluster/status", commands)

        clustered = {"ovnnb_db.db": "clustered\n", "ovnsb_db.db": "clustered\n"}
        result, commands = self.run_healthcheck("192.0.2.1", clustered)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("cluster/status", commands)
        self.assertNotIn("get-db-storage-status", commands)

    def test_unsafe_or_unknown_modes_stop_before_starting_servers(self):
        cases = [
            ("", {"ovnsb_db.db": "clustered"}),
            ("192.0.2.1", {"ovnnb_db.db": "corrupted"}),
            ("192.0.2.1", {"ovnnb_db.db": "standalone", "ovnsb_db.db": "clustered"}),
            ("192.0.2.1,", {}),
        ]
        for addresses, databases in cases:
            with self.subTest(addresses=addresses, databases=databases):
                result, commands, after = self.run_start(addresses, databases=databases)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn("ERROR!", result.stderr)
                self.assertNotIn("ovn-ctl", commands)
                self.assertEqual(after, databases)


if __name__ == "__main__":
    unittest.main()
