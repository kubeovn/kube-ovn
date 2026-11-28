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
        self.peerAddress = "unix:" + str(self.root / "peer")
        self.cid = str(uuid.uuid4())
        self.peerSid = str(uuid.uuid4())
        self.log = self.root / "log"

    def command(self, *args):
        return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT, timeout=5)

    def start(self, singleMember=False):
        schema = self.root / "schema"
        schema.write_text(json.dumps(SCHEMA))
        if singleMember:
            self.command("ovsdb-tool", "create-cluster", str(self.database), str(schema), self.address)
        else:
            self.command("ovsdb-tool", "--cid=" + self.cid, "join-cluster",
                         str(self.database), SCHEMA["name"], self.address, self.peerAddress)
        self.sid = self.command("ovsdb-tool", "db-sid", str(self.database)).strip()
        self.startServer()

    def startServer(self):
        output = self.log.open("a")
        self.addCleanup(output.close)
        self.process = subprocess.Popen(
            [os.environ.get("OVSDB_SERVER", "ovsdb-server"), "--no-chdir",
             "--unixctl=" + str(self.control), "--pidfile=" + str(self.root / "pid"),
             str(self.database)], stdout=output, stderr=output,
        )
        self.addCleanup(self.stop)
        self.waitFor(lambda: self.control.exists() and (self.root / "raft").exists())

    def stop(self):
        if self.process.poll() is None:
            self.process.terminate()
            self.process.wait(timeout=5)

    def waitFor(self, condition, timeout=5):
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
        self.send("hello_request", address=self.peerAddress)

    def send(self, method, **params):
        params.update({"cluster": self.cid, "from": self.peerSid, "to": self.sid})
        message = {"method": method, "params": [params], "id": None}
        self.connection.sendall(json.dumps(message).encode() + b"\n")

    def snapshot(self, servers):
        self.send("install_snapshot_request", term=9, last_index=1, last_term=1,
                  last_eid=str(uuid.uuid4()), last_servers=servers,
                  election_timer=100, data=[SCHEMA, {}])
        self.waitFor(lambda: "Log: [2, 2]" in self.status())

    def append(self, entries, commit):
        self.send("append_request", term=9, prev_log_index=1, prev_log_term=1,
                  leader_commit=commit, log=entries)

    def heartbeatBarrier(self):
        self.send("append_request", term=9, prev_log_index=1, prev_log_term=1,
                  leader_commit=1, log=[], comment="heartbeat")
        decoder = json.JSONDecoder()
        buffer = ""
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            buffer = buffer.lstrip()
            try:
                message, end = decoder.raw_decode(buffer)
            except json.JSONDecodeError:
                chunk = self.connection.recv(65536)
                self.assertTrue(chunk, self.log.read_text())
                buffer += chunk.decode()
                continue
            buffer = buffer[end:]
            if (message.get("method") == "append_reply"
                    and message["params"][0].get("comment") == "heartbeat"):
                return
        self.fail("heartbeat was not acknowledged:\n" + self.log.read_text())

    def checkSingletonRejoin(self, replyFirst, restart=False, historicalConfig=False):
        self.start()
        self.connect()
        if replyFirst:
            self.send("add_server_reply", success=True)
        self.snapshot({self.sid: self.address})
        if not replyFirst:
            self.send("add_server_reply", success=True)
        # Reset the timer to the snapshot's 100 ms value. The random election
        # range is 1000 ms; withhold further replication beyond both.
        if historicalConfig:
            self.append([
                {"term": 1, "servers": {self.sid: self.address, self.peerSid: self.peerAddress}},
                {"term": 1, "servers": {self.sid: self.address}},
            ], 3)
            self.waitFor(lambda: "Log: [2, 4]" in self.status())
        if restart:
            self.stop()
            self.connection.close()
            self.startServer()
            self.connect()
        self.append([], 1)
        self.waitFor(lambda: "is leader for term 9" in self.log.read_text()
                     or "Role: leader" in self.status())
        deadline = time.monotonic() + 1.5
        while time.monotonic() < deadline:
            self.assertNotIn("Role: leader", self.status(), self.log.read_text())
            time.sleep(0.05)
        self.assertIn("Status: joining cluster", self.status())
        self.assertIn("Term: 9\n", self.status())

        # The committed configuration must finish joining without another
        # add-server reply once replication resumes.
        if historicalConfig:
            self.send("append_request", term=9, prev_log_index=3, prev_log_term=1,
                      leader_commit=4, log=[{"term": 9, "servers": {
                          self.sid: self.address, self.peerSid: self.peerAddress,
                      }}])
        else:
            self.append([{"term": 9, "servers": {
                self.sid: self.address, self.peerSid: self.peerAddress,
            }}], 2)
        self.waitFor(lambda: "Status: cluster member" in self.status())
        self.assertIn("Role: follower", self.status())
        self.stop()
        self.command("ovsdb-tool", "check-cluster", str(self.database))

    def test_replyBeforeHistoricalSingletonSnapshot(self):
        self.checkSingletonRejoin(replyFirst=True)

    def test_replyAfterHistoricalSingletonSnapshot(self):
        self.checkSingletonRejoin(replyFirst=False)

    def test_restartBeforeConfigurationCatchup(self):
        self.checkSingletonRejoin(replyFirst=True, restart=True)

    def test_historicalSingletonConfigurationEntry(self):
        self.checkSingletonRejoin(replyFirst=True, historicalConfig=True)

    def test_partialHistoricalConfigurationCatchup(self):
        self.start()
        self.connect()
        self.snapshot({self.sid: self.address})
        self.send("add_server_reply", success=True)
        oldPeer = str(uuid.uuid4())
        self.send("append_request", term=9, prev_log_index=1, prev_log_term=1,
                  leader_commit=4, log=[{"term": 1, "servers": {
                      self.sid: self.address, oldPeer: "unix:" + str(self.root / "oldPeer"),
                  }}])
        self.waitFor(lambda: "Log: [2, 3]" in self.status())
        self.assertIn("Status: joining cluster", self.status())
        self.send("append_request", term=9, prev_log_index=2, prev_log_term=1,
                  leader_commit=4, log=[{"term": 1, "servers": {self.sid: self.address}}])
        self.waitFor(lambda: "Log: [2, 4]" in self.status())
        deadline = time.monotonic() + 1.5
        while time.monotonic() < deadline:
            self.assertNotIn("Role: leader", self.status(), self.log.read_text())
            time.sleep(0.05)
        self.assertIn("Status: joining cluster", self.status())
        self.send("append_request", term=9, prev_log_index=3, prev_log_term=1,
                  leader_commit=4, log=[{"term": 9, "servers": {
                      self.sid: self.address, self.peerSid: self.peerAddress,
                  }}])
        self.waitFor(lambda: "Status: cluster member" in self.status())
        self.stop()
        self.command("ovsdb-tool", "check-cluster", str(self.database))

    def test_replyWithMultiMemberSnapshot(self):
        self.start()
        self.connect()
        self.snapshot({self.sid: self.address, self.peerSid: self.peerAddress})
        self.append([], 1)
        self.send("add_server_reply", success=True)
        self.waitFor(lambda: "Status: cluster member" in self.status())
        self.assertIn("Role: follower", self.status())
        self.stop()
        self.startServer()
        self.assertIn("Status: cluster member", self.status())

    def test_replyBeforeOwnMembership(self):
        self.start()
        self.connect()
        other = str(uuid.uuid4())
        servers = {self.peerSid: self.peerAddress,
                   other: "unix:" + str(self.root / "other")}
        self.snapshot(servers)
        self.append([], 1)
        self.send("add_server_reply", success=True)
        self.heartbeatBarrier()
        self.assertIn("Status: joining cluster", self.status())
        self.stop()
        self.connection.close()
        self.startServer()
        self.connect()
        self.assertIn("Status: joining cluster", self.status())
        servers[self.sid] = self.address
        self.append([{"term": 9, "servers": servers}], 2)
        self.waitFor(lambda: "Status: cluster member" in self.status())
        self.stop()
        self.startServer()
        self.assertIn("Status: cluster member", self.status())

    def test_newSingleMemberClusterCanElect(self):
        self.start(singleMember=True)
        self.waitFor(lambda: "Role: leader" in self.status(), timeout=8)


if __name__ == "__main__":
    unittest.main(verbosity=2)
