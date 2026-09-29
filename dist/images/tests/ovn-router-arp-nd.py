#!/usr/bin/env python3
"""Check router-owned ARP/ND and flood suppression using the built OVN binaries."""

import os
from pathlib import Path
import subprocess
import tempfile
import time


def arp(target, source="192.0.2.1", port="localnet.ext", mac="00:00:00:00:00:01"):
    return (
        f'inport == "{port}" && eth.src == {mac} && '
        "eth.dst == ff:ff:ff:ff:ff:ff && arp.op == 1 && "
        f"arp.spa == {source} && arp.tpa == {target} && arp.sha == {mac} && "
        "arp.tha == 00:00:00:00:00:00"
    )


def ns(suffix):
    target = int(suffix, 16)
    return (
        'inport == "localnet.ext" && eth.src == 00:00:00:00:00:01 && '
        f"eth.dst == 33:33:ff:00:{target >> 8:02x}:{target & 255:02x} && "
        f"ip6.src == 2001:db8:1::1 && ip6.dst == ff02::1:ff00:{suffix} && "
        "ip.ttl == 255 && icmp6.type == 135 && icmp6.code == 0 && "
        f"nd.target == 2001:db8:1::{suffix} && nd.sll == 00:00:00:00:00:01"
    )


def createTopology(nb, sb):
    sb("chassis-add", "gw", "geneve", "192.0.2.10")
    nb("ls-add", "ext", "--", "lsp-add", "ext", "localnet.ext", "--",
       "lsp-set-type", "localnet.ext", "localnet", "--",
       "lsp-set-addresses", "localnet.ext", "unknown", "--",
       "lsp-set-options", "localnet.ext", "network_name=physnet")
    nb("lr-add", "hub", "--", "lrp-add", "hub", "hub-ext",
       "00:00:00:00:01:01", "192.0.2.2/24", "2001:db8:1::2/64", "--",
       "lrp-set-gateway-chassis", "hub-ext", "gw", "100", "--",
       "lsp-add", "ext", "ext-hub", "--", "lsp-set-type", "ext-hub", "router", "--",
       "lsp-set-addresses", "ext-hub", "router", "--",
       "lsp-set-options", "ext-hub", "router-port=hub-ext")
    nb("lsp-add", "ext", "vif", "--", "lsp-set-addresses", "vif",
       "00:00:00:00:03:03 192.0.2.30", "--", "set", "Logical_Switch_Port", "vif", "up=true")
    for kind, external, internal in (
        ("snat", "192.0.2.100", "10.0.0.0/24"),
        ("dnat", "192.0.2.110", "10.0.0.10"),
        ("dnat_and_snat", "192.0.2.111", "10.0.0.11"),
        ("snat", "2001:db8:1::100", "fd10::/64"),
    ):
        nb("lr-nat-add", "hub", kind, external, internal)
    for name, vip, backend in (
        ("lb4", "192.0.2.120:80", "10.0.0.20:80"),
        ("lb6", "[2001:db8:1::120]:80", "[fd10::20]:80"),
    ):
        nb("lb-add", name, vip, backend, "tcp", "--", "lr-lb-add", "hub", name)


def checkFlooding(nb, trace):
    def expect(packet, text, present=True):
        output = trace(packet)
        if (text in output) != present:
            raise AssertionError(f"Expected {text!r} present={present}:\n{output}")
        return output

    # Keep northd running: option changes must replace old flows, not just
    # produce the right result after a daemon restart.
    for snoop in ("false", "true"):
        nb("set", "Logical_Switch", "ext", "other_config:mcast_snoop=" + snoop)
        for flood in ("unset", "false", "true", "false"):
            if flood == "unset":
                nb("remove", "NB_Global", ".", "options", "bcast_arp_nd_req_flood")
            else:
                nb("set", "NB_Global", ".", "options:bcast_arp_nd_req_flood=" + flood)
            nb("--wait=sb", "sync")
            for target in ("100", "110", "111", "120", "2", "30"):
                expect(arp("192.0.2." + target), "arp.op = 2;")
            expect(arp("192.0.2.100", source="0.0.0.0"), "arp.op = 2;")
            expect(arp("192.0.2.100").replace(
                "eth.dst == ff:ff:ff:ff:ff:ff", "eth.dst == 00:00:00:00:01:01"
            ), "arp.op = 2;")
            for target in ("100", "120"):
                expect(ns(target), "nd_na {")
            expect(arp("192.0.2.100", source="192.0.2.100", port="ext-hub",
                       mac="00:00:00:00:01:01"), 'outport = "_MC_flood_l2";')
            unknown = (
                arp("192.0.2.200", source="0.0.0.0"),
                arp("192.0.2.200", source="0.0.0.0", port="vif", mac="00:00:00:00:03:03"),
                arp("192.0.2.200", source="192.0.2.2", port="ext-hub", mac="00:00:00:00:01:01"),
                ns("200"),
            )
            for packet in unknown:
                output = trace(packet)
                if flood == "true":
                    assert 'outport = "_MC_flood' in output, output
                else:
                    assert 'outport = "_MC_unknown";' in output, output
                    assert 'outport = "_MC_flood' not in output, output
            if flood != "true":
                expect(arp("192.0.2.100") + " && flags[1] == 1",
                       'ingress(dp="hub"', present=False)
            print(f"PASS: router ARP/ND, GARP and probes: snooping={snoop}, flood={flood}", flush=True)


    # Without unknown ports the suppression rule must not affect the switch.
    nb("lsp-del", "localnet.ext", "--", "lsp-add", "ext", "peer", "--",
       "lsp-set-addresses", "peer", "00:00:00:00:00:01 192.0.2.1", "--",
       "set", "Logical_Switch_Port", "peer", "up=true", "--",
       "set", "NB_Global", ".", "options:bcast_arp_nd_req_flood=false")
    nb("--wait=sb", "sync")
    expect(arp("192.0.2.100", port="peer"), "arp.op = 2;")
    expect(arp("192.0.2.200", port="peer"), 'outport = "_MC_flood')
    print("PASS: switch without unknown ports retains native ARP handling", flush=True)


def runTest(directory):
    directory = Path(directory)
    env = dict(os.environ, OVN_RUNDIR=str(directory), OVS_RUNDIR=str(directory))
    database = "unix:" + str(directory / "db.sock")
    processes = []

    def run(*args):
        return subprocess.check_output(
            args, env=env, text=True, stderr=subprocess.STDOUT, timeout=20
        )

    def start(name, *args):
        with (directory / (name + ".log")).open("w") as log:
            processes.append(subprocess.Popen(
                [name, *args], env=env, stdout=log, stderr=log
            ))

    def nb(*args):
        return run("ovn-nbctl", "--timeout=10", "--db=" + database, *args)

    def sb(*args):
        return run("ovn-sbctl", "--timeout=10", "--db=" + database, *args)

    def trace(packet):
        return run("ovn-trace", "--db=" + database, "ext", packet)

    try:
        schemas = Path(env.get("OVN_PKGDATADIR", "/usr/share/ovn"))
        for name in ("nb", "sb"):
            run("ovsdb-tool", "create", str(directory / (name + ".db")),
                str(schemas / ("ovn-" + name + ".ovsschema")))
        start("ovsdb-server", "--remote=punix:" + str(directory / "db.sock"),
              "--unixctl=" + str(directory / "db.ctl"),
              str(directory / "nb.db"), str(directory / "sb.db"))
        deadline = time.monotonic() + 10
        while not (directory / "db.sock").exists():
            if processes[0].poll() is not None or time.monotonic() > deadline:
                raise RuntimeError("ovsdb-server did not create its socket")
            time.sleep(0.05)
        nb("init")
        sb("init")
        start("ovn-northd", "--ovnnb-db=" + database, "--ovnsb-db=" + database,
              "--unixctl=" + str(directory / "northd.ctl"))
        createTopology(nb, sb)
        checkFlooding(nb, trace)
    except Exception as error:
        if isinstance(error, subprocess.CalledProcessError):
            print(error.output, flush=True)
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
    with tempfile.TemporaryDirectory(prefix="ovn-arp-nd-") as directory:
        runTest(directory)
