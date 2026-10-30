#!/usr/bin/env python3
"""Configure source-preserving NAT for the IPv4, non-Globalnet Kind E2E pair."""

import argparse
import ipaddress
import json
import re
import subprocess
import time


TABLE = "kube_ovn_submariner_e2e"


def run(command, inputText=None):
    return subprocess.run(command, input=inputText, text=True, check=True,
                          stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=30).stdout


def kubectl(cluster, *args):
    return ["kubectl", "--context", "kind-" + cluster, *args]


def ipv4CIDR(value):
    network = ipaddress.ip_network(value)
    if network.version != 4:
        raise ValueError("Submariner Kind E2E NAT setup requires IPv4 CIDRs")
    return str(network)


def clusterSubnets(cluster):
    items = json.loads(run(kubectl(cluster, "get", "subnet", "ovn-default", "join", "-o", "json")))["items"]
    return {item["metadata"]["name"]: item for item in items}


def policyPatch(subnet, destinations):
    if not subnet["spec"].get("natOutgoing"):
        raise ValueError("Expected natOutgoing=true on the Kind E2E Pod subnet")
    rule = {"match": {"dstIPs": ",".join(destinations)}, "action": "forward"}
    existing = subnet["spec"].get("natOutgoingPolicyRules", [])
    # Rules are ordered: the peer exemption must precede broader NAT policies.
    rules = [rule] + [item for item in existing if item != rule]
    return {"metadata": {"resourceVersion": subnet["metadata"]["resourceVersion"]},
            "spec": {"natOutgoingPolicyRules": rules}}


def proxyConfig(node):
    # Read the running proxy's effective config, including the defaulted mark bit.
    config = json.loads(run(["docker", "exec", node, "curl", "--fail", "--silent", "--show-error",
                            "--max-time", "5", "http://127.0.0.1:10249/configz"]))
    for item in config.values():
        if isinstance(item, dict) and "mode" in item:
            return item
    raise ValueError("kube-proxy /configz did not include an effective proxy mode")


def serviceRules(localPodCIDR, peerPodCIDR, serviceCIDR, masqueradeBit):
    if isinstance(masqueradeBit, bool) or not isinstance(masqueradeBit, int) or not 0 <= masqueradeBit <= 31:
        raise ValueError("Invalid kube-proxy nftables masqueradeBit")
    mark = 1 << masqueradeBit
    clearMask = 0xffffffff ^ mark
    # A filter hook runs for every packet, before kube-proxy's srcnat hook (100).
    # iifname survives route-agent interface recreation. Only remote Pod traffic
    # DNATed from a local Service qualifies; HostNetworking retains its SNAT.
    rule = (f'iifname "vxlan-tunnel" ip saddr {ipv4CIDR(peerPodCIDR)} '
            f'ip daddr {ipv4CIDR(localPodCIDR)} ct status dnat '
            f'ct original ip daddr {ipv4CIDR(serviceCIDR)} '
            f'meta mark & {mark:#x} != 0 meta mark set meta mark & {clearMask:#x} '
            'counter comment "Submariner Kind E2E remote Pod to Service"')
    return f"""add table ip {TABLE}
add chain ip {TABLE} service_no_masq {{ type filter hook postrouting priority 99; policy accept; }}
flush chain ip {TABLE} service_no_masq
add rule ip {TABLE} service_no_masq {rule}
"""


def waitFor(description, check, timeout):
    deadline = time.monotonic() + timeout
    lastError = "condition not met"
    while True:
        try:
            result = check()
            if result:
                return result
        except subprocess.CalledProcessError as error:
            lastError = error.stderr.strip()
        if time.monotonic() >= deadline:
            raise RuntimeError(f"Timed out waiting for {description}: {lastError}")
        time.sleep(2)


def policyReady(cluster, expected):
    subnet = json.loads(run(kubectl(cluster, "get", "subnet", "ovn-default", "-o", "json")))
    return any(rule.get("ruleID") and rule.get("match") == expected["match"] and
               rule.get("action") == "forward" for rule in subnet.get("status", {}).get("natOutgoingPolicyRules", []))


def nodePolicyReady(node, podCIDR, destinations):
    rules = run(["docker", "exec", node, "nft", "list", "chain", "ip", "kube-ovn", "nat-policy"])
    match = re.search(r"ip saddr " + re.escape(podCIDR) + r" ip daddr @([\w-]+) accept", rules)
    if not match:
        return False
    for destination in destinations:
        address = str(ipaddress.ip_network(destination).network_address)
        run(["docker", "exec", node, "nft", "get", "element", "ip", "kube-ovn", match[1], "{", address, "}"])
    return True


def configure(cluster, remoteCluster, serviceCIDR, remoteServiceCIDR, timeout):
    local = clusterSubnets(cluster)
    remote = clusterSubnets(remoteCluster)
    localPodCIDR = ipv4CIDR(local["ovn-default"]["spec"]["cidrBlock"])
    peerPodCIDR = ipv4CIDR(remote["ovn-default"]["spec"]["cidrBlock"])
    destinations = [peerPodCIDR, ipv4CIDR(remote["join"]["spec"]["cidrBlock"]), ipv4CIDR(remoteServiceCIDR)]
    localNetworks = [localPodCIDR, ipv4CIDR(local["join"]["spec"]["cidrBlock"]), ipv4CIDR(serviceCIDR)]
    if any(ipaddress.ip_network(a).overlaps(ipaddress.ip_network(b)) for a in localNetworks for b in destinations):
        raise ValueError("The non-Globalnet Kind E2E clusters must have non-overlapping networks")
    nodes = run(["kind", "get", "nodes", "--name", cluster]).split()
    if not nodes:
        raise RuntimeError(f"No Kind nodes found for {cluster}")

    patch = policyPatch(local["ovn-default"], destinations)
    run(kubectl(cluster, "patch", "subnet", "ovn-default", "--type=merge", "-p", json.dumps(patch)))
    waitFor(cluster + " subnet NAT policy", lambda: policyReady(cluster, patch["spec"]["natOutgoingPolicyRules"][0]), timeout)

    for node in nodes:
        config = waitFor(node + " kube-proxy config", lambda: proxyConfig(node), timeout)
        if config["mode"] != "nftables":
            if config["mode"] not in ("", "ipvs", "iptables"):
                raise ValueError(f"Unsupported kube-proxy mode on {node}: {config['mode']}")
            print(f"{node}: {config['mode'] or 'iptables'} uses Submariner's iptables exemption", flush=True)
            continue
        waitFor(node + " Kube-OVN peer NAT exemption",
                lambda: nodePolicyReady(node, localPodCIDR, destinations), timeout)
        rules = serviceRules(localPodCIDR, peerPodCIDR, serviceCIDR, config["nftables"]["masqueradeBit"])
        run(["docker", "exec", "-i", node, "nft", "--check", "-f", "-"], rules)
        run(["docker", "exec", "-i", node, "nft", "-f", "-"], rules)
        print(f"{node}: peer Pod/Service NAT exemptions ready", flush=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cluster", required=True)
    parser.add_argument("--remote-cluster", required=True)
    parser.add_argument("--service-cidr", required=True)
    parser.add_argument("--remote-service-cidr", required=True)
    parser.add_argument("--timeout", type=int, default=120)
    args = parser.parse_args()
    try:
        configure(args.cluster, args.remote_cluster, args.service_cidr, args.remote_service_cidr, args.timeout)
    except (ValueError, KeyError, StopIteration, RuntimeError, subprocess.SubprocessError) as error:
        parser.exit(1, f"Submariner E2E NAT setup failed: {error}\n{getattr(error, 'stderr', '')}\n")


if __name__ == "__main__":
    main()
