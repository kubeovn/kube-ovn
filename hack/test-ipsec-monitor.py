#!/usr/bin/env python3
"""Exercise the installed monitor's connection parser and keyless intent."""

import ast
import ipaddress
import json
import os
from pathlib import Path
import re
import sys
import tempfile
from types import MethodType, SimpleNamespace
import unittest


def methods(source, class_name, names, namespace):
    tree = ast.parse(source)
    cls = next(node for node in tree.body if isinstance(node, ast.ClassDef) and node.name == class_name)
    selected = [node for node in cls.body if isinstance(node, ast.FunctionDef) and node.name in names]
    if len(selected) != len(names):
        raise AssertionError("installed monitor lacks the required methods")
    exec(compile(ast.Module(body=selected, type_ignores=[]), "installed-monitor", "exec"), namespace)
    return {name: namespace[name] for name in names}


class MonitorContract(unittest.TestCase):
    prefix = "ko" + "A" * 26 + "-"
    interface = "ovn-peer2-0"

    def test_current_children_are_not_mistaken_for_obsolete_interfaces(self):
        current = self.prefix + self.interface + "-in-3"
        obsolete = self.prefix + self.interface + "-out-2"
        foreign = "ko" + "B" * 26 + "-" + self.interface + "-in-3"
        status = "\n".join((current + "[1]: ESTABLISHED", current + "{11}: INSTALLED",
                            obsolete + "{9}: INSTALLED", foreign + "{7}: INSTALLED",
                            "foreign-in-1{7}: INSTALLED", current + "{8}garbage: malformed"))
        calls = []

        def run_command(args, _description):
            calls.append(args)
            return (0, status if args[-1] == "status" else "", "")

        monitor = SimpleNamespace(connection_prefix=self.prefix, applied_pki=None,
                                  conf_in_use={"pki": {"certificate": None, "ca_cert": None}},
                                  tunnels={self.interface: SimpleNamespace(version=3)},
                                  persist_connection_intent=lambda: calls.append(["persist"]))
        namespace = {"re": re, "monitor": monitor, "run_command": run_command,
                     "vlog": SimpleNamespace(info=lambda *_: None, err=lambda *_: None)}
        functions = methods(SOURCE, "StrongSwanHelper", ("get_active_conns", "refresh"), namespace)
        helper = SimpleNamespace(IPSEC="/usr/sbin/ipsec")
        helper.get_active_conns = MethodType(functions["get_active_conns"], helper)
        actual = helper.get_active_conns()
        self.assertEqual(set(actual), {self.interface})
        self.assertEqual(set(actual[self.interface]), {current + "[1]", current + "{11}", obsolete + "{9}"})
        calls.clear()
        functions["refresh"](helper, monitor)
        self.assertEqual(calls[0], ["persist"], "intent must be durable before IKE loads configuration")
        terminated = [args[-1] for args in calls if "down-nb" in args]
        self.assertEqual(terminated, [obsolete + "{9}"], "current or foreign connections must not be terminated")

    def test_intent_records_versions_and_refuses_a_different_lease(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "intent.json"
            monitor = SimpleNamespace(ovn_owned_only=True, connection_prefix=self.prefix,
                                      connection_owner={"version": 1, "nodeUID": "node-uid", "lease": "lease",
                                                        "prefix": self.prefix, "mark": 759811, "reqid": 759811},
                                      connection_intent={}, connection_intent_file=str(path))
            namespace = {"os": os, "json": json, "tempfile": tempfile, "ipaddress": ipaddress, "re": re}
            functions = methods(SOURCE, "IPsecMonitor", ("record_connection_intent", "persist_connection_intent"), namespace)
            tunnel = SimpleNamespace(name=self.interface, version=3,
                                     conf={"custom_options": {"reqid": "759811", "mark_out": "759811/0xffffffff"},
                                           "interface_uuid": "75980000-0000-0000-0000-000000000001",
                                           "local_ip": "192.0.2.1", "remote_ip": "192.0.2.2", "tunnel_type": "geneve",
                                           "private_key": "never-export-this-path", "psk": "never-export-this-value"})
            functions["record_connection_intent"](monitor, tunnel)
            tunnel.version = 4
            functions["record_connection_intent"](monitor, tunnel)
            functions["persist_connection_intent"](monitor)
            data = json.loads(path.read_text())
            self.assertEqual(len(data["connections"]), 4, "asynchronous deletion must not lose older intent")
            self.assertEqual(data["nodeUID"], "node-uid")
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertNotIn("never-export", path.read_text())
            tunnel.conf["custom_options"]["reqid"] = "99"
            with self.assertRaisesRegex(RuntimeError, "protection selectors"):
                functions["record_connection_intent"](monitor, tunnel)
            self.assertEqual(path.read_text(), json.dumps(data, sort_keys=True), "a conflicting lease must not replace recorded intent")


if __name__ == "__main__":
    SOURCE = Path(sys.argv.pop(1)).read_text()
    unittest.main()
