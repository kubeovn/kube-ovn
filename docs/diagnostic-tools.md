# Production diagnostic tool boundary

The production Kube-OVN image keeps the networking daemons and the small
`kubectl-ko-node-agent` binary. Node diagnostics run in the independent agent,
which is privileged only for the operation requested by `kubectl ko`.

| Previous image utility | Production replacement | Notes |
| --- | --- | --- |
| `tcpdump`, sniffer utilities | `kubectl ko capture` | The agent opens an `AF_PACKET` socket in the target network namespace and can stream a pcap (`-w -`) without `nsenter` or a `tcpdump` process. |
| Wireshark | Local workstation pcap reader | Capture output remains a pcap stream; decode it on the workstation. A GUI is not needed on a node. |
| `netcat`/`ncat` | `kubectl ko diagnose connectivity` | The Go TCP/UDP probe reports endpoint reachability without shipping a general-purpose listener. |
| `nmap` Geneve probe | Native agent UDP probe | `diagnose environment` reports `open`, `closed`, or `unknown`; an unanswered UDP packet is not treated as proof of readiness. |
| `strace`, `readelf`, `gdb` | Structured `kubectl ko` trace, logs, network inspection and performance commands | These tools are not required by production images. The debug image may continue to install `gdb`, `valgrind` and packet tools for CI and interactive debugging. |

Capture accepts `-i`, `-c`, `-s`, `-nn` and `-w -`. Unsupported packet-filter
expressions fail before a remote request is opened. Save pcap streams on the
workstation and inspect them there.

The agent enters a mounted network namespace itself with `setns(2)`. This
keeps packet capture independent from `kube-ovn-cni`, `ovs-ovn` and
`ovn-central` containers. The agent remains Linux-only because `AF_PACKET` and
network namespaces are Linux kernel interfaces.
