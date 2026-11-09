#!/usr/bin/env python3
"""Exercise the packaged localport ARP exception with a dummy OVS datapath."""

import json
import os
from pathlib import Path
import re
import struct
import subprocess
import tempfile
import time


def frames(path):
    if not path.exists():
        return []
    data = path.read_bytes()
    if len(data) < 24:
        return []
    byte_order = "<" if data[:4] in (b"\xd4\xc3\xb2\xa1", b"\x4d\x3c\xb2\xa1") else ">"
    result = []
    offset = 24
    while offset + 16 <= len(data):
        length = struct.unpack_from(byte_order + "I", data, offset + 8)[0]
        if offset + 16 + length > len(data):
            break
        result.append(data[offset + 16:offset + 16 + length].hex())
        offset += 16 + length
    return result


def run_test(directory):
    directory = Path(directory)
    env = dict(os.environ, OVN_RUNDIR=str(directory), OVS_RUNDIR=str(directory))
    database = "unix:" + str(directory / "db.sock")
    processes = []

    def run(*args):
        return subprocess.check_output(args, env=env, text=True, stderr=subprocess.STDOUT, timeout=20)

    def start(name, *args):
        with (directory / (name + ".log")).open("w") as log:
            process = subprocess.Popen([name, *args], env=env, stdout=log, stderr=log)
            processes.append(process)

    def wait(description, condition):
        deadline = time.monotonic() + 20
        while True:
            if any(process.poll() is not None for process in processes):
                raise RuntimeError("a daemon exited while waiting for " + description)
            if condition():
                return
            if time.monotonic() >= deadline:
                raise AssertionError("timed out waiting for " + description)
            time.sleep(0.05)

    def nb(*args):
        return run("ovn-nbctl", "--timeout=10", "--db=" + database, *args)

    def sb(*args):
        return run("ovn-sbctl", "--timeout=10", "--db=" + database, *args)

    def ovs(*args):
        return run("ovs-vsctl", "--no-wait", "--timeout=10", "--db=" + database, *args)

    def flows(table):
        return run("ovs-ofctl", "dump-flows", "br-int", "table=" + str(table))

    def counter(table, priority):
        return sum(int(re.search(r"n_packets=(\d+)", line).group(1))
                   for line in flows(table).splitlines()
                   if re.search(r"priority=" + str(priority) + r"(?:,|\s)", line))

    def inject(interface, packet):
        run("ovs-appctl", "-t", str(directory / "vswitchd.ctl"),
            "netdev-dummy/receive", interface, packet)

    def captured(path, packet):
        # A datapath can pad short Ethernet frames to the minimum wire length.
        return any(frame.startswith(packet) for frame in frames(path))

    try:
        schemas = Path(env.get("OVN_PKGDATADIR", "/usr/share/ovn"))
        ovs_schema = Path(env.get("OVS_PKGDATADIR", "/usr/share/openvswitch"))
        for name in ("nb", "sb"):
            run("ovsdb-tool", "create", str(directory / (name + ".db")),
                str(schemas / ("ovn-" + name + ".ovsschema")))
        run("ovsdb-tool", "create", str(directory / "ovs.db"), str(ovs_schema / "vswitch.ovsschema"))
        start("ovsdb-server", "--remote=punix:" + str(directory / "db.sock"),
              "--unixctl=" + str(directory / "db.ctl"),
              *(str(directory / (name + ".db")) for name in ("nb", "sb", "ovs")))
        wait("OVSDB socket", lambda: (directory / "db.sock").exists())
        nb("init")
        sb("init")
        ovs("init")
        ovs("set", "Open_vSwitch", ".", "external_ids:system-id=localport-test",
            "external_ids:ovn-remote=" + database,
            "external_ids:ovn-encap-type=geneve", "external_ids:ovn-encap-ip=127.0.0.1",
            "external_ids:ovn-bridge-mappings=phys:br-phys,isolated:br-isolated")
        for bridge in ("br-int", "br-phys", "br-isolated"):
            ovs("add-br", bridge, "--", "set", "Bridge", bridge, "datapath_type=dummy")
        ovs("set-fail-mode", "br-int", "secure")
        wire = directory / "wire.pcap"
        speaker = directory / "speaker.pcap"
        ovs("add-port", "br-phys", "wire0", "--", "set", "Interface", "wire0",
            "type=dummy", "options:tx_pcap=" + str(wire))
        ovs("add-port", "br-int", "mlb0", "--", "set", "Interface", "mlb0",
            "type=dummy", "external_ids:iface-id=speaker", "options:tx_pcap=" + str(speaker))
        ovs("add-port", "br-int", "backend0", "--", "set", "Interface", "backend0",
            "type=dummy", "external_ids:iface-id=backend")
        start("ovs-vswitchd", "--enable-dummy=override", "--unixctl=" + str(directory / "vswitchd.ctl"), database)
        start("ovn-northd", "--ovnnb-db=" + database, "--ovnsb-db=" + database,
              "--unixctl=" + str(directory / "northd.ctl"))
        start("ovn-controller", "--unixctl=" + str(directory / "controller.ctl"), database)
        nb("ls-add", "underlay", "--", "lsp-add", "underlay", "speaker", "--",
           "lsp-set-type", "speaker", "localport", "--", "lsp-set-addresses", "speaker", "unknown", "--",
           "lsp-add", "underlay", "localnet", "--", "lsp-set-type", "localnet", "localnet", "--",
           "lsp-set-addresses", "localnet", "unknown", "--", "lsp-set-options", "localnet", "network_name=phys", "--",
           "lsp-add", "underlay", "backend", "--", "lsp-set-addresses", "backend", "02:00:00:00:00:02 192.0.2.2", "--",
           "set", "NB_Global", ".", "options:ls_dnat_mod_dl_dst=true")
        wait("backend binding", lambda: nb("lsp-get-up", "backend").strip() == "up")
        nb("--wait=hv", "sync")
        # The same backend MAC rewrite is required by the DPDK image as well.
        actions = json.loads(sb("--format=json", "--columns=actions", "list", "Logical_Flow"))["data"]
        assert ['eth.dst = 02:00:00:00:00:02; outport = "backend"; output;'] in actions, "missing underlay DNAT MAC rewrite"

        garp = "ffffffffffff02000000000108060001080006040001020000000001c0000264000000000000c0000264"
        before = counter(47, 160)
        inject("mlb0", garp)
        wait("default localport ARP drop", lambda: counter(47, 160) > before)
        assert not captured(wire, garp), "default localport leaked ARP to localnet"
        print("PASS: default unknown localport suppresses physical ARP", flush=True)

        nb("set", "Logical_Switch_Port", "speaker", "options:localnet-arp-only=true")
        nb("--wait=hv", "sync")
        inject("mlb0", garp)
        wait("outbound ARP", lambda: captured(wire, garp))
        reply = "02000000000302000000000108060001080006040002020000000001c0000264020000000003c0000203"
        inject("mlb0", reply)
        wait("outbound ARP reply", lambda: captured(wire, reply))
        request = "ffffffffffff02000000000308060001080006040001020000000003c0000203000000000000c0000264"
        inject("wire0", request)
        wait("inbound ARP", lambda: captured(speaker, request))
        print("PASS: opted-in localport sends GARP/replies and receives ARP requests", flush=True)

        outbound_ip = "0200000000030200000000010800450000140000000040010000c0000264c0000203"
        before = counter(0, 190)
        inject("mlb0", outbound_ip)
        wait("outbound IP drop", lambda: counter(0, 190) > before)
        assert not captured(wire, outbound_ip), "speaker emitted IP to localnet"
        inbound_ip = "0200000000010200000000030800450000140000000040010000c0000203c0000264"
        before = counter(65, 110)
        inject("wire0", inbound_ip)
        wait("inbound IP drop", lambda: counter(65, 110) > before)
        assert not captured(speaker, inbound_ip), "unknown IP reached the speaker"
        print("PASS: actual IP frames hit the drop rules in both directions", flush=True)

        port_key = sb("--bare", "--columns=tunnel_key", "find", "Port_Binding", "logical_port=speaker").strip()
        assert not sb("--bare", "--columns=_uuid", "find", "FDB", "port_key=" + port_key).strip(), "localport learned an FDB entry"
        nb("remove", "Logical_Switch_Port", "speaker", "options", "localnet-arp-only")
        nb("--wait=hv", "sync")
        count = len(frames(wire))
        before = counter(47, 160)
        inject("mlb0", garp)
        wait("restored localport ARP drop", lambda: counter(47, 160) > before)
        assert len(frames(wire)) == count, "removing the option failed to restore suppression"
        print("PASS: no localport FDB learning; removing the option restores suppression", flush=True)

        # A Cluster-policy announcer may have no backend on this logical
        # switch. The opted-in localport must make its datapath local itself.
        isolated_wire = directory / "isolated-wire.pcap"
        ovs("add-port", "br-isolated", "wire1", "--", "set", "Interface", "wire1",
            "type=dummy", "options:tx_pcap=" + str(isolated_wire))
        ovs("add-port", "br-int", "mlb1", "--", "set", "Interface", "mlb1",
            "type=dummy", "external_ids:iface-id=isolated-speaker")
        nb("ls-add", "isolated", "--", "lsp-add", "isolated", "isolated-speaker", "--",
           "lsp-set-type", "isolated-speaker", "localport", "--",
           "lsp-set-addresses", "isolated-speaker", "unknown", "--",
           "lsp-set-options", "isolated-speaker", "localnet-arp-only=true", "--",
           "lsp-add", "isolated", "isolated-localnet", "--",
           "lsp-set-type", "isolated-localnet", "localnet", "--",
           "lsp-set-addresses", "isolated-localnet", "unknown", "--",
           "lsp-set-options", "isolated-localnet", "network_name=isolated")
        nb("--wait=hv", "sync")
        inject("mlb1", garp)
        wait("ARP without a local backend", lambda: captured(isolated_wire, garp))
        print("PASS: opted-in localport works without a local backend or router port", flush=True)
    except Exception as error:
        if isinstance(error, subprocess.CalledProcessError):
            print(error.output, flush=True)
        for table in (0, 47, 65):
            print(flows(table), flush=True)
        for log in directory.glob("*.log"):
            print(log.name + ":\n" + log.read_text(), flush=True)
        raise
    finally:
        for process in reversed(processes):
            if process.poll() is None:
                process.terminate()
                try:
                    process.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()


if __name__ == "__main__":
    with tempfile.TemporaryDirectory(prefix="ovn-localport-") as directory:
        run_test(directory)
