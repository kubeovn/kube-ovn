# EIP NetworkPolicy and address reuse regression

Run `hack/eip-network-policy-e2e.sh` against an installed Kube-OVN 1.16+
cluster with a configured external gateway and a Docker network connected to it:

```sh
EIP=172.19.0.236 \
EXTERNAL_NETWORK=kube-ovn-vlan \
KUBE_OVN_IMAGE=kubeovn/kube-ovn:v1.16.1 \
ARTIFACT_DIR=/tmp/eip-policy-results \
bash hack/eip-network-policy-e2e.sh
```

Reserve the test EIP outside other allocations. The script creates its own
namespace, Pod and external client; it does not configure the gateway or change
global conntrack options. It is an opt-in integration test, not automatically
executed by the regular Go E2E suites.

The test checks:

1. EIP connectivity before applying policy.
2. An existing ICMP flow is blocked after applying a TCP-only NetworkPolicy,
   without alternating packet leaks (#7935).
3. Connectivity returns after removing the policy.
4. Deleting the Pod withdraws its annotation-based EIP NAT: an external probe
   must explicitly report three requests and zero replies before reuse.
5. Recreating the Pod with the same EIP permits a **new** ICMP flow.
6. Final cleanup again verifies that the deleted EIP stops replying.

## Conntrack semantics on reuse (#7962)

An EIP becoming unreachable after deletion does not mean every NAT conntrack
entry has expired. An existing tuple may retain its previous Pod mapping after
reassignment. Matching traffic refreshes the idle timer, even if the old Pod no
longer replies. This is distinct from the tiered ACL leakage fixed by the OVN
patch in this change.

In the tested kernel dataplane, `net.netfilter.nf_conntrack_icmp_timeout` was
30 seconds with no OVS CT timeout policy. The old NAT entry was present at
29.31 seconds after pausing traffic and absent at 31.41 seconds. Resuming the
same ping then automatically established the new mapping and restored replies.
A second run also recovered after 33 seconds of inactivity. Timeout values are
protocol- and environment-dependent, not a fixed EIP deletion grace period.

The reuse check therefore starts a new ping process in the same external client
namespace (a new ICMP identifier), rather than requiring migration of an old
NAT tuple. It does not flush conntrack or alter system timeout settings. No
production change to NAT expiration is needed for the observed behavior.

Artifacts include ping output, the deletion/reuse probes and resource YAML.
The convergence and observation windows can be adjusted with
`POLICY_SETTLE_SECONDS` and `OBSERVE_SECONDS`.
