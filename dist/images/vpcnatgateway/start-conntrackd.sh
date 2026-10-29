#!/usr/bin/env bash
set -eo pipefail

# conntrackd sidecar of an HA VPC NAT gateway.
#
# Every replica of an HA gateway NATs the traffic of the same VPC, but each one keeps its own
# conntrack table. A reply coming back through another replica than the one that NATed the request,
# or a replica replacing a dead one, then has no entry to translate the packet with and drops it.
# conntrackd multicasts the entries of this Pod to the other replicas and writes the entries it
# receives straight into the kernel table (DisableExternalCache), so any replica can handle any
# flow. A starting daemon asks its peers for a full dump (StartupResync), which is what a
# replacement Pod relies on to get the table back.
#
# This container shares the network namespace, and therefore the conntrack table and the iptables
# rules, with the vpc-nat-gw container.
#
# Only IPv4 entries are synchronized: the gateway NATs IPv4 only (nat-gateway.sh writes no
# ip6tables rule), so an IPv6 flow crossing it is routed, not translated, and survives a failover
# without any state. The channel itself is IPv4 too, bound to the IPv4 address of the gateway
# subnet interface even when that subnet is dual-stack.
#
# Environment:
#   POD_IPS                   - addresses of the gateway subnet interface, comma-separated; the
#                               IPv4 one is used as the source of the synchronization messages.
#                               Never an EIP: EIPs move between replicas.
#   CONNTRACK_SYNC_INTERFACE  - interface the entries are synchronized over (default: eth0)
#   CONNTRACK_SYNC_GROUP      - IPv4 multicast group the replicas synchronize over
#   CONNTRACK_SYNC_PORT       - UDP port the replicas synchronize over (default: 3780)

SYNC_INTERFACE=${CONNTRACK_SYNC_INTERFACE:-eth0}
SYNC_GROUP=${CONNTRACK_SYNC_GROUP}
SYNC_PORT=${CONNTRACK_SYNC_PORT:-3780}
# conntrackd -s, which the readiness probe runs, reads the default path, so the rendered
# configuration has to land there. CONNTRACKD_CONF only exists so that tests can write elsewhere.
CONNTRACKD_CONF=${CONNTRACKD_CONF:-/etc/conntrackd/conntrackd.conf}

if [ -z "$SYNC_GROUP" ]; then
    >&2 echo "Error: CONNTRACK_SYNC_GROUP is not set"
    exit 1
fi

# 224.0.0.0/4, with 224.0.0.0/24 excluded: it carries the link-local control groups.
if ! echo "$SYNC_GROUP" | grep -qE '^(22[4-9]|23[0-9])\.[0-9]+\.[0-9]+\.[0-9]+$' || \
   echo "$SYNC_GROUP" | grep -qE '^224\.0\.0\.[0-9]+$'; then
    >&2 echo "Error: CONNTRACK_SYNC_GROUP $SYNC_GROUP is not a usable IPv4 multicast address"
    exit 1
fi

if ! ip link show "$SYNC_INTERFACE" >/dev/null 2>&1; then
    >&2 echo "Error: sync interface '$SYNC_INTERFACE' not found"
    exit 1
fi

# The downward API gives the addresses of the primary interface, which is the gateway subnet one.
# On a dual-stack subnet it lists both families, in an order that depends on the cluster: pick the
# IPv4 one rather than the first one. Read the interface itself when the Pod IPs are not
# published, e.g. in tests.
SYNC_IP=$(echo "${POD_IPS}" | tr ',' '\n' | grep -E '^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$' | head -n1 || true)
if [ -z "$SYNC_IP" ]; then
    SYNC_IP=$(ip -4 -o addr show dev "$SYNC_INTERFACE" | awk '{print $4}' | cut -d/ -f1 | head -n1)
fi
if [ -z "$SYNC_IP" ]; then
    >&2 echo "Error: no IPv4 address on sync interface '$SYNC_INTERFACE', conntrack synchronization needs an IPv4 gateway subnet"
    exit 1
fi

echo "synchronizing conntrack entries from $SYNC_IP over $SYNC_GROUP:$SYNC_PORT on $SYNC_INTERFACE"

# Pick the same iptables backend as nat-gateway.sh so that both write to the same rule set.
iptables_cmd=$(which iptables)
if iptables-legacy -t nat -S INPUT 1 2>/dev/null >/dev/null; then
    iptables_cmd=$(which iptables-legacy)
fi

# Insert a rule once, at the head of its chain.
function ensure_rule() {
    local table=$1 chain=$2
    shift 2
    if ! $iptables_cmd -t "$table" -C "$chain" "$@" 2>/dev/null; then
        $iptables_cmd -t "$table" -I "$chain" "$@"
    fi
}

# The synchronization traffic has to reach the daemon whatever the gateway does with the VPC
# traffic: accept it explicitly, keep it out of the conntrack table (the entries would be
# replicated back to the peers), and out of the mangle PREROUTING chain, whose mark sends the
# traffic received on the VPC interface to the policy routing table of the external network.
ensure_rule filter INPUT -i "$SYNC_INTERFACE" -p udp -d "$SYNC_GROUP" --dport "$SYNC_PORT" -j ACCEPT
ensure_rule filter OUTPUT -o "$SYNC_INTERFACE" -p udp -d "$SYNC_GROUP" --dport "$SYNC_PORT" -j ACCEPT
ensure_rule raw PREROUTING -i "$SYNC_INTERFACE" -p udp -d "$SYNC_GROUP" --dport "$SYNC_PORT" -j CT --notrack
ensure_rule raw OUTPUT -o "$SYNC_INTERFACE" -p udp -d "$SYNC_GROUP" --dport "$SYNC_PORT" -j CT --notrack
ensure_rule mangle PREROUTING -i "$SYNC_INTERFACE" -p udp -d "$SYNC_GROUP" --dport "$SYNC_PORT" -j ACCEPT

# A synchronized entry carries the TCP window state of the replica that created it, which does not
# match what this replica observes: without a liberal tracker the kernel marks the packets of a
# recovered flow INVALID. Both sysctls live in the network namespace shared with the gateway.
for sysctl in net.netfilter.nf_conntrack_tcp_be_liberal net.netfilter.nf_conntrack_tcp_loose; do
    if ! sysctl -w "$sysctl=1" >/dev/null 2>&1; then
        >&2 echo "WARNING: failed to set $sysctl, flows recovered from a peer may be seen as invalid"
    fi
done

mkdir -p "$(dirname "$CONNTRACKD_CONF")"
cat > "$CONNTRACKD_CONF" <<EOF
# Generated by start-conntrackd.sh, do not edit.
Sync {
    # FTFW resends the updates a peer has not acknowledged, so an entry is not lost when a
    # synchronization message is dropped on the gateway subnet.
    Mode FTFW {
        # Commit the entries received from the peers to the kernel table immediately instead of
        # holding them in the external cache until a failover: every replica is active.
        DisableExternalCache on
        # Ask the peers for their table at startup, so a replacement Pod recovers the flows.
        StartupResync on
    }

    Multicast {
        IPv4_address $SYNC_GROUP
        # conntrackd calls the UDP port of the multicast channel its group.
        Group $SYNC_PORT
        IPv4_interface $SYNC_IP
        Interface $SYNC_INTERFACE
        SndSocketBuffer 1249280
        RcvSocketBuffer 1249280
        Checksum on
    }
}

General {
    HashSize 32768
    HashLimit 262144
    LogFile off
    Syslog off
    LockFile /var/lock/conntrackd.lock

    UNIX {
        Path /var/run/conntrackd.ctl
    }

    # A gateway tracks the flows of a whole VPC: a small netlink buffer loses events under load.
    NetlinkBufferSize 2097152
    NetlinkBufferSizeMaxGrowth 8388608

    # Filtering in kernel space drops the irrelevant events before they wake the daemon up.
    Filter From Kernelspace {
        # The protocols the gateway translates. Entries of any other protocol, SCTP and DCCP
        # included, stay local to the replica that created them.
        Protocol Accept {
            tcp
            udp
            icmp
        }

        # Flows this replica must not export: the ones of another address family, since the
        # gateway only NATs IPv4, and the ones that belong to this replica alone, its own BFD
        # sessions and API connections and the synchronization traffic itself.
        Address Ignore {
            IPv6_address ::/0
            IPv4_address 127.0.0.1
            IPv4_address $SYNC_IP
            IPv4_address $SYNC_GROUP
        }
    }
}
EOF

exec conntrackd -C "$CONNTRACKD_CONF"
