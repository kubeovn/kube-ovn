# NodePort failure diagnostics

The connectivity suite runs continuous HTTP requests from the runner to a
NodePort on a node other than the backend's node while another Ginkgo process
restarts or pauses OVN/OVS components. A single failed request still fails the
suite; the request timeout remains five seconds.

Every failed request logs its index, UTC start time, duration, TCP connection
setup, local/remote socket addresses, request write, and first response byte.
Use these to distinguish a connect timeout from a request that was written but
never received response headers. Trace hooks do not retry requests or drain
response bodies, so connection reuse behavior is unchanged.

To also collect packet captures and cluster snapshots in a local **kind** run:

```bash
export E2E_NODEPORT_DIAGNOSTICS_DIR="$PWD/nodeport-diagnostics"
make kube-ovn-connectivity-e2e
```

This requires Docker access, `tcpdump`, `nsenter`, and passwordless `sudo`.
The connectivity CI job enables this for both overlay and underlay and uploads
`nodeport-diagnostics-overlay` / `nodeport-diagnostics-underlay`, including on
successful runs for comparison.

Artifacts:

- `target.txt`: namespace, Service, NodePort, and backend port. The snapshots
  identify the backend IP/node and Service endpoints.
- `runner.pcap*` and `<node>.pcap*`: runner and kind node network namespaces,
  captured before disruption until suite cleanup. Node captures survive OVN/OVS
  Pod restarts. Filters include the test's TCP ports, Geneve, ARP, and ICMP.
  Each capture uses at most four 8 MB files with 160-byte packet snapshots.
- `baseline/` and `first-failure/`: Service, EndpointSlice, backend Pod, component
  placement, events, routes, neighbors, socket state, iptables/nftables counters,
  Linux and OVS conntrack, OVS interfaces/flows, and NB load balancers.
- `<node>-runtime-logs.txt`: the last 2,000 lines of each node's ovn-controller
  and ovs-vswitchd log after disruption, retained on successful runs as well.
- `*-tcpdump.log`, `stop.log`, and individual snapshot files: errors and command
  timestamps and exit codes for each snapshot command, including commands inside
  node/OVS groups. A failed command is recorded and later commands still run.
  Pod discovery keeps this metadata on stderr so its stdout remains a Pod list.
  Missing tools, absent Pods, and command timeouts are retained.

Start with the failed request's socket tuple and time in the Ginkgo log. Locate
its SYN/request and replies in the runner capture, then follow the translated
flow through the ingress and backend nodes. Compare endpoint readiness, NAT
counters, conntrack, and OVS interface/flow state with the baseline, and align
with the disruption steps in the Ginkgo timeline. A timeout's HTTP trace alone
does not establish which part of the data path dropped traffic.

Only the first failure starts a snapshot, asynchronously so traffic continues.
The suite waits for it before deleting its Service and backend. Individual
commands have deadlines; the helper has a 45-second deadline. Packet capture
has a 30-minute safety limit, and a CI cleanup step also stops it if the test
process exits early. Diagnostics are best effort and do not change assertions.
The baseline is collected before either parallel process begins its tests.
The OVS restart case waits for a different Pod UID with no deletion timestamp
and a true Pod Ready condition before allowing the next disruption to start.
