#!/usr/bin/env python3
"""Test the real Raft receiver with a successful join and an old snapshot.

Requires ovsdb-server, ovsdb-tool and ovs-appctl on PATH. No root or cluster
is needed; a local Unix socket supplies the peer's Raft messages.
"""

import json
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import unittest
import uuid


SCHEMA = {
    "name": "RejoinTest",
    "version": "1.0.0",
    "tables": {"T": {"columns": {"value": {"type": "integer"}}, "isRoot": True}},
}


class RejoinTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory(prefix="raft-rejoin-")
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)
        self.database = self.root / "db"
        self.control = self.root / "ctl"
        self.address = "unix:" + str(self.root / "raft")
        self.peer_address = "unix:" + str(self.root / "peer")
        self.cid = str(uuid.uuid4())
        self.peer_sid = str(uuid.uuid4())
        self.log = self.root / "log"

    def command(self, *args):
        return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT, timeout=5)

    def start(self, single_member=False):
        schema = self.root / "schema"
        schema.write_text(json.dumps(SCHEMA))
        if single_member:
            self.command("ovsdb-tool", "create-cluster", str(self.database), str(schema), self.address)
        else:
            self.command("ovsdb-tool", "--cid=" + self.cid, "join-cluster",
                         str(self.database), SCHEMA["name"], self.address, self.peer_address)
        self.sid = self.command("ovsdb-tool", "db-sid", str(self.database)).strip()
        output = self.log.open("w")
        self.addCleanup(output.close)
        self.process = subprocess.Popen(
            [os.environ.get("OVSDB_SERVER", "ovsdb-server"), "--no-chdir",
             "--unixctl=" + str(self.control), "--pidfile=" + str(self.root / "pid"),
             str(self.database)], stdout=output, stderr=output,
        )
        self.addCleanup(self.stop)
        self.wait_for(lambda: self.control.exists() and (self.root / "raft").exists())

    def stop(self):
        if self.process.poll() is None:
            self.process.terminate()
            self.process.wait(timeout=5)

    def wait_for(self, condition, timeout=5):
        deadline = time.monotonic() + timeout
        while time.monotonic() < deadline:
            self.assertIsNone(self.process.poll(), self.log.read_text())
            if condition():
                return
            time.sleep(0.02)
        self.fail("condition did not converge:\n" + self.log.read_text())

    def status(self):
        return self.command("ovs-appctl", "-t", str(self.control), "cluster/status", SCHEMA["name"])

    def connect(self):
        self.connection = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self.addCleanup(self.connection.close)
        self.connection.settimeout(5)
        self.connection.connect(str(self.root / "raft"))
        self.send("hello_request", address=self.peer_address)

    def send(self, method, **params):
        params.update({"cluster": self.cid, "from": self.peer_sid, "to": self.sid})
        message = {"method": method, "params": [params], "id": None}
        self.connection.sendall(json.dumps(message).encode() + b"\n")

    def snapshot(self, servers):
        self.send("install_snapshot_request", term=9, last_index=1, last_term=1,
                  last_eid=str(uuid.uuid4()), last_servers=servers,
                  election_timer=100, data=[SCHEMA, {}])
        self.wait_for(lambda: "Log: [2, 2]" in self.status())

    def append(self, entries, commit):
        self.send("append_request", term=9, prev_log_index=1, prev_log_term=1,
                  leader_commit=commit, log=entries)

    def check_singleton_rejoin(self, reply_first):
        self.start()
        self.connect()
        if reply_first:
            self.send("add_server_reply", success=True)
        self.snapshot({self.sid: self.address})
        if not reply_first:
            self.send("add_server_reply", success=True)
        # Reset the timer to the snapshot's 100 ms value. The random election
        # range is 1000 ms; withhold further replication beyond both.
        self.append([], 1)
        self.wait_for(lambda: "is leader for term 9" in self.log.read_text())
        deadline = time.monotonic() + 1.5
        while time.monotonic() < deadline:
            self.assertNotIn("Role: leader", self.status(), self.log.read_text())
            time.sleep(0.05)
        self.assertIn("Status: joining cluster", self.status())
        self.assertIn("Term: 9\n", self.status())

        # The committed configuration must finish joining without another
        # add-server reply once replication resumes.
        self.append([{"term": 9, "servers": {
            self.sid: self.address, self.peer_sid: self.peer_address,
        }}], 2)
        self.wait_for(lambda: "Status: cluster member" in self.status())
        self.assertIn("Role: follower", self.status())
        self.stop()
        self.command("ovsdb-tool", "check-cluster", str(self.database))

    def test_reply_before_historical_singleton_snapshot(self):
        self.check_singleton_rejoin(reply_first=True)

    def test_reply_after_historical_singleton_snapshot(self):
        self.check_singleton_rejoin(reply_first=False)

    def test_reply_with_multi_member_snapshot(self):
        self.start()
        self.connect()
        self.snapshot({self.sid: self.address, self.peer_sid: self.peer_address})
        self.send("add_server_reply", success=True)
        self.wait_for(lambda: "Status: cluster member" in self.status())
        self.assertIn("Role: follower", self.status())

    def test_new_single_member_cluster_can_elect(self):
        self.start(single_member=True)
        self.wait_for(lambda: "Role: leader" in self.status(), timeout=8)


if __name__ == "__main__":
    unittest.main(verbosity=2)
