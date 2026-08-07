# Experimental NetworkPolicy ACL sampling

Kube-OVN can sample OVN ACL decisions generated for
`networking.k8s.io/v1` `NetworkPolicy`. The feature is experimental and
disabled by default. It is intended for short-lived troubleshooting and
observability workflows, not as a replacement for enforcement logs or a
general packet collector.

The initial implementation distinguishes two decisions:

- `allow`: a stateful `allow-related` ACL generated for a specific ingress or
  egress NetworkPolicy rule;
- `default-deny`: a generated drop ACL that applies when no allow rule
  matches.

A default-deny event may name the NetworkPolicy that owns the sampled ACL, but
that attribution is non-exclusive. When several policies select the same Pod,
the sampled default-deny ACL is not proof that one specific policy caused the
denial. Default-deny events therefore have no rule index.

## Requirements and limitations

Controller-side ACL sampling requires OVN 24.09 or later. Node-local delivery
additionally requires all of the following:

- OVS 3.4 or later;
- the Linux kernel datapath, not the userspace or DPDK datapath;
- `psample=true` in the active OVS datapath capabilities;
- an OVS schema containing
  `Flow_Sample_Collector_Set.local_group_id`;
- a Linux kernel with the `psample` generic-netlink family available.

Only Kube-OVN ACLs generated for Kubernetes NetworkPolicy rule allows and
NetworkPolicy default deny are eligible. DHCP exceptions, gateway and node
ACLs, SecurityGroups, AdminNetworkPolicy, BaselineAdminNetworkPolicy,
ClusterNetworkPolicy, and other ACL producers are excluded.

Sampling does not flush conntrack. Existing connections follow OVN's current
conntrack state and might not produce a new-session sample after the feature
is enabled.

## Enable the feature

Both Helm charts expose the same `aclSampling` values. For the recommended v2
chart, use for example:

```yaml
aclSampling:
  enabled: true
  setID: 142
  localGroupID: 142
  appIDNew: 102
  appIDEstablished: 103
  collectorIDAllow: 1
  collectorIDDefaultDeny: 2
  allowProbabilityPercent: 1
  defaultDenyProbabilityPercent: 100
```

```bash
helm upgrade --install kube-ovn ./charts/kube-ovn-v2 \
  --set aclSampling.enabled=true
```

Use `./charts/kube-ovn` instead to configure the classic chart. Keep the
controller and CNI agent values identical across split or independently
managed deployments.

For the manifest-based installer, use the equivalent environment variables:

```bash
ENABLE_ACL_SAMPLING=true \
ACL_SAMPLING_ALLOW_PROBABILITY_PERCENT=1 \
ACL_SAMPLING_DEFAULT_DENY_PROBABILITY_PERCENT=100 \
bash dist/images/install.sh
```

The remaining variable names are the upper snake-case forms of the Helm
fields, prefixed with `ACL_SAMPLING_`, for example
`ACL_SAMPLING_LOCAL_GROUP_ID` and `ACL_SAMPLING_APP_ID_ESTABLISHED`.

The configuration fields have the following semantics:

| Field | Constraint | Purpose |
| --- | --- | --- |
| `enabled` | Boolean | Enables controller objects, ACL attachment, and node-local delivery. |
| `setID` | Non-zero unsigned 32-bit integer | Shared OVN collector set and OVS collector-set ID. |
| `localGroupID` | Unsigned 32-bit integer; zero is valid | Local Linux psample group carried by the OVS collector set. |
| `appIDNew` | Unique integer from 1 to 255 | Application byte for `acl-new` samples. |
| `appIDEstablished` | Unique integer from 1 to 255 | Application byte for `acl-est` samples. |
| `collectorIDAllow` | Unique integer from 1 to 255 | Collector for allow decisions. |
| `collectorIDDefaultDeny` | Unique integer from 1 to 255 | Collector for default-deny decisions. |
| `allowProbabilityPercent` | Number from 0 through 100 | Sampling probability for allow decisions. |
| `defaultDenyProbabilityPercent` | Number from 0 through 100 | Sampling probability for default-deny decisions. |

Kube-OVN converts a percentage to OVN's 16-bit probability with
`round(percent * 65535 / 100)`. A zero probability disables attachment for
that decision. The default 100% default-deny probability can produce a high
event rate during scans or attacks; lower it for sustained use.

Configured IDs must not collide with objects owned by another application.
Kube-OVN may reuse a matching unowned `Sampling_App`, but does not modify or
delete it. Conflicting unowned applications, collectors, or node collector
sets make sampling unavailable and leave those objects unchanged.

## Decode and listen

Decode a decimal or `0x`-prefixed hexadecimal sample metadata value or OVS
user cookie:

```bash
kubectl ko acl-sample decode 0x640abcde000000c8
```

Listen on one node and decode each received event through the OVN northbound
leader:

```bash
kubectl ko acl-sample listen --node worker-1
```

The listener reads the enabled CNI DaemonSet's `localGroupID`, selects a
running `kube-ovn-cni` Pod on the requested node, and emits one YAML document
per decoded event. A sample that no longer resolves in NBDB is reported to
standard error without stopping the listener.

An OVN local-sampling cookie is encoded as:

```text
63                         32 31                           0
+----------------------------+-----------------------------+
| observation domain         | Sample.metadata             |
+----------------------------+-----------------------------+

observation domain = application ID (8 bits) | datapath key (24 bits)
```

Metadata-only input can identify the ACL and policy, but does not identify
whether the event came from `acl-new` or `acl-est` and does not carry the
logical datapath key.

The decoded YAML includes the policy UID, namespace, name, direction and rule
index for allow events; the OVN ACL UUID, action, direction, priority, tier
and match hash; and the sampling application and metadata. Default-deny output
uses `policyOwner`, sets `reason: network-policy-default-deny` and
`attribution: non-exclusive`, and omits the rule index.

## Security

Linux psample messages include captured packet bytes. The current debug
command only emits the cookie and decoded policy event, but the node-local
process still receives packet data from the kernel. Access to
`kubectl ko acl-sample listen` therefore grants a packet-observation
capability and should be limited to authorized operators. Use the lowest
practical sampling probabilities, protect command output as sensitive data,
and stop the listener when troubleshooting is complete.

## Failure isolation and monitoring

Sampling is best-effort. Kube-OVN commits NetworkPolicy enforcement before it
attaches sampling in a separate transaction. An unsupported schema, ID
conflict, metadata allocation failure, node capability failure, or attachment
failure must not prevent a valid NetworkPolicy from taking effect.

Monitor the following metrics and controller/daemon warning logs:

| Metric | Meaning |
| --- | --- |
| `acl_sampling_controller_available` | `1` when the enabled controller sampling path last reconciled successfully; `0` when disabled or unavailable. |
| `acl_sampling_controller_failures_total{operation}` | Best-effort controller failures for `reconcile`, `prepare`, or `attach`. |
| `acl_sampling_node_available{node_name}` | `1` when local delivery is enabled and available on the node; `0` otherwise. |
| `acl_sampling_node_failures_total{node_name}` | Node collector-set reconciliation failures. |

## Disable and cleanup

Set `aclSampling.enabled` back to `false` on both controller and agents.
Kube-OVN then clears sample references only from ACLs marked as its
NetworkPolicy sampling ACLs, allows OVN to garbage-collect unreferenced
`Sample` rows, deletes only unreferenced collectors and applications it owns,
and removes only its owned node collector sets. Reused or conflicting unowned
objects and references are preserved.
