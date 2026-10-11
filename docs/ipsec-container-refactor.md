# Node IPsec container refactor

This branch builds on kubeovn/kube-ovn#7593. When IPsec is enabled, the
`kube-ovn-cni` Pod runs an additional `ipsec` container using the same image.
The container runs `/kube-ovn/kube-ovn-ipsec`; CNI no longer watches the CA
Secret, issues certificates, starts IPsec services, or flushes host XFRM state.

The new container owns the persistent key directory and needs `NET_ADMIN`,
`NET_BIND_SERVICE`, and `SYS_NICE`. Its subprocesses run with a configurable
nice priority, defaulting to -5. CNI explicitly drops `SYS_NICE` and uses the
unprivileged service user even with IPsec enabled. Installer debug mode retains
its existing CNI root exception. Pod-level host networking, host PID namespace,
and the ServiceAccount remain shared; this is not a separate security identity.
Both Charts and the installer explicitly use a zero-surge CNI DaemonSet rollout;
the persistent node owner lock additionally excludes concurrent new writers.

The node module persists pending private keys, watches Kubernetes CSR results,
validates the returned identity, and
activates complete key/certificate/trust generations through a single OVSDB
map mutation. Other `other_config` entries are preserved. Failed renewals keep
an existing valid identity. The monitor and strongSwan starter run as supervised
foreground processes with private monitor pid/control paths. The monitor does
not restart the IKE daemon itself.

Renewals use a stable per-node/certificate schedule between 40% and 60% of the
leaf lifetime, so simultaneously installed nodes do not all renew at the midpoint.
Repeated runtime startup failures back off from one to thirty seconds; a runtime
that remained up for a minute resets that delay before recovery.

The base-image build applies a standard source patch to the upstream OVS monitor
before creating the OVS packages:
accept CN at the end of an RFC2253 subject, optionally filter interfaces by
their owning Port's `ovn-chassis-id`, and report public configuration digests
through `configuration/get` after successful strongSwan update/secret reload.
The IPsec entrypoint enables the filter. Readiness waits for the current
certificate/trust digest acknowledgement as well as responsive processes;
changed public content invalidates the previous acknowledgement. This confirms
configuration refresh, not replacement of every established peer SA or a
cluster CA rotation acknowledgement.
The private status endpoint reports the validated Node UID, local chassis,
generation and public certificate/trust digests. `configurationApplied` is
computed against that exact current configuration under the runtime lock and
requires a healthy runtime. Activation invalidates the previous confirmation
before changing trust or OVSDB, so a probe cannot carry confirmation across a
partially committed generation. These fields contain no PEM or key material.
Image construction fails if the source patch no longer applies to the monitor.
Connection generation and refresh remain in OVS. The filter
does not prove ownership of orphaned kernel SAs after a crash.

Each supervised IKE/monitor pair now gets a fresh random connection namespace,
bound to the current Node UID and protection lease. The private persistent
`connections/<namespace>/session.json` is committed before starting IKE;
`intent.json` records exact generated connection names, Interface UUIDs,
underlay addresses, tunnel type, reqid and output mark before configuration is
loaded. Older sessions and obsolete versions remain recorded, so a restart
cannot silently reuse names or overwrite earlier evidence. The monitor refuses
connection selectors from a different lease. These files contain no PEM,
private-key paths, PSKs or SA keys. Intent alone cannot authorize an orphaned SA
deletion.

The supervisor also records a private `sa-ledger.json` per runtime session.
It brackets a kernel snapshot with two private IKE status reads and accepts
only unchanged installed CHILD_SA names with durable intent. Both SPIs must
match exact kernel endpoints, ESP transport mode, reqid, output mark and UDP
selectors. The ledger contains explicit public metadata, including host boot
identity, kernel installation time and NAT-T encapsulation; it never embeds or
serializes a netlink `XfrmState`, which contains SA keys. Rekeyed/retired
instances remain recorded. A conflicting, ambiguous or unsupported observation
produces no new binding. These observations are not a deletion transaction:
kernel lifetime granularity, later tuple reuse, unobserved installs and the
coordination cleanup protocol must still be handled before orphan deletion is
safe. This draft does not yet delete kernel SAs based on the ledger.

After stopping both process groups of a supervised pair, the runtime checks the
live kernel before recording a private `drain.json` observation. Both unchanged
guards must remain installed. Remaining SAs or policies using the lease's reqid
or an overlapping mark block the observation, including installations missed
by periodic IKE snapshots. A current-boot historical endpoint/SPI collision also
blocks completion even when AddTime or key material has changed. A previous
boot's tuple cannot identify the new kernel; live lease conflicts still count.
These checks never delete an SA or policy. Disjoint foreign resources remain
untouched. The observation is historical evidence, not a cleanup receipt or
authorization to remove protection: coordinated disable must repeat live checks.

The strongSwan status parser also matches complete connection suffixes. Its
previous greedy interface match could classify `ovn-peer-in-3{11}` as interface
`ovn-peer-in`, causing a currently configured child to be treated as obsolete.
The installed-monitor contract checks that only the exact obsolete owned
connection is terminated and that foreign namespaces are ignored. Real IKE
and candidate traffic tests must additionally confirm the adapted naming.

The built-in signer checks the CSR signature, bound Pod identity, live
DaemonSet ownership, Node UID, and the requested chassis CN/SAN. A shared
ServiceAccount or a request name alone is not sufficient. Clusters that do not
populate the bound Pod authentication fields must not silently fall back to
name-based approval. The node's remaining Kubernetes privileges and chassis
registration still need to be included in the threat model.

Both Charts use an `ipsec` configuration section for the backend, issuer,
duration, timeout, priority, and resource limits. Existing IPsec feature switches
remain in place. The controller selects the signing backend with
`--cert-manager-ipsec-cert` and `--cert-manager-issuer-name`. Nodes always submit
a Kubernetes CSR, including when cert-manager signs the certificate. The
controller validates the bound identity before forwarding an approved CSR to
cert-manager and checks the returned certificate before publishing it. CNI's
ServiceAccount cannot create CertificateRequests; that permission belongs only
to the controller and is restricted to its namespace.

The IPsec entrypoint owns `--ovn-ipsec-cert-duration`, `--request-timeout`, and
`--priority`, rather than `kube-ovn-daemon`.

The external ClusterIssuer and public `ovn-ipsec-ca` trust bundle must be
provisioned consistently. The issuer must be dedicated to Kube-OVN IPsec.
Install Kube-OVN with IPsec disabled first, then deploy cert-manager on the
ordinary Pod network and wait for its components and ClusterIssuer to be Ready.
When enabling IPsec, the initial Prepare barrier issues and validates every
node identity while that network remains available. Only after every frozen
node acknowledges its identity does Arm install protection and start the owned
runtime, followed by the global encryption switch. No host-network override
for cert-manager is required. Existing protection remains armed during restarts
and rotations; a new Prepare never authorizes a downgrade of an active lease.
Prepare reserves private ownership before writing pending identities without
installing kernel guards or publishing output marks. This lets a cancelled
first enable complete the same guarded cleanup/release protocol even when no
certificate was issued; an inactive reservation distinguishes preparation
from a previously completed release.
Installation creates a fail-closed ValidatingAdmissionPolicy and binding that
restrict requests to this ClusterIssuer across all namespaces to the controller
ServiceAccount in its configured namespace. Kubernetes >= 1.30 is required for
this backend. Metadata-only updates are allowed; other requesters cannot change
the authorized request's spec. Before forwarding, the controller checks the
policy scope, observed generation and unconditional Deny binding. Missing or
weakened policy is retryable and never forwards a request. Generic cert-manager
auto-approval cannot admit a different requester past this policy.
Policy installation propagation still requires cluster acceptance testing
before this draft is ready.

The disposable API test has passed the bound-token and issuer rejection/issuance
contract. This does not establish installation safety across API Server replicas:
`observedGeneration` confirms policy processing, not that every instance already
enforces a newly created binding. The admission barrier must be established before
the dedicated issuer is made available. That rollout barrier remains incomplete
in this draft; a later immediate rejection check returned an accepted request after policy
status convergence. The test now waits for an actual unauthorized dry-run
rejection before provisioning its disposable issuer. This does not implement
a production barrier across API Server instances.

`hack/test-ipsec-runtime.sh` validates the real candidate's startup, private
endpoints, priority/capability inheritance, monitor crash recovery, and shutdown.
`hack/test-ipsec-traffic.sh` runs two private network namespaces with synthetic
certificate identities and UDP payloads on Geneve/VXLAN ports, for IPv4 and IPv6.
Each peer first waits for the production startup gate and then uses its actual
mark/reqid lease, including setting the socket mark before connecting. A fixed
test reqid must not race bootstrap or bypass the production ownership checks.
A separate fixture captures the outer interface and requires ESP packets with
zero plaintext transport packets. The isolated test also prototypes a
low-priority XFRM block policy before starting IKE and confirms it survives
runtime shutdown. Production uses marked guards described below rather than this unmarked
address/UDP selector. This verifies synthetic Linux transport/IKE; it does not run
`ovn-controller` or Pod overlays, or establish production protection during
faults and rollout.

`hack/test-ipsec-api.sh` uses a disposable CI cluster with real cert-manager to
check bound Pod authentication fields, built-in signing, rendered issuer
admission and controller forwarding. It intentionally grants its test CNI
CertificateRequest privileges to verify admission independently of RBAC;
production CNI does not have those privileges. The test also checks namespace
restriction, unrelated issuers and metadata-only updates. This is an API
contract test, not a full Kube-OVN deployment or migration test.

`hack/test-ipsec-overlay.sh` installs the candidate in a disposable two-node
Kind cluster, exchanges actual cross-node Pod traffic in both directions, and
checks the underlay capture for ESP and absence of plaintext tunnel packets.
Its outcome must be recorded against the tested image and revision; the harness
itself is not evidence of a successful deployment or fault-safe migration.

Local probes use the private status socket with `--check=livez` or
`--check=readyz`. Readiness includes an unexpired active certificate and runtime
health; API outages do not directly fail liveness. An IPsec readiness failure
still makes the whole Pod unready, although its CNI container is not restarted.

With the feature disabled, both Charts and the installer use an ordinary
`ipsec-cleanup` init container, with no restartPolicy, probes or resident cleanup
process. It exits successfully immediately when there is no previous IPsec state.
It runs as root only during initialization, with `NET_ADMIN` and
`NET_BIND_SERVICE`, without `SYS_NICE`. It never starts IKE/monitor, issues an
encryption certificate, reads a CA, or imports a legacy identity. A fresh
disabled installation allocates no protection lease. Persistent directories retain ownership
evidence; the CNI server cannot mount them.

For an actual disable, the controller persists a Disabling challenge and waits
for synchronous native NB/SB reads to confirm both encryption switches are off.
It freezes the cleanup init template and database identities, then authorizes
Release without waiting for every init to run simultaneously. Each init restores
its Node-UID-bound reservation and serves the independent OVS Pod's startup
gate while checking local tunnel convergence, owned connection/SA drain and
identity references. Lost evidence, foreign owners or an incomplete drain retain
protection and block that node's initialization. It repeats live checks around
its bound-Pod CSR and withdraws only its own output lease and two guards before
publishing a signed released receipt and exiting. An ordinary blocking init does
not block OVS, which runs in a separate DaemonSet.
For a frozen node cancelled before Prepare reserved ownership, the live disable
challenge authorizes a new guarded reservation so it can attest release too.

The controller verifies every frozen node's signed released receipt against the
live bound Pod, CSR, complete init/ordinary container template and database
challenge before recording Disabled. This supports `maxUnavailable=1`: one
node's init exits before the next Pod is replaced. Successfully completed init
containers retain their verified result for the same live Pod/template and
Release epoch; running or failed init containers cannot complete the Release barrier.
No controller-created DaemonSet or additional Kubernetes permissions are used.
Any failed local cleanup keeps its node guarded for retry. The controller does
not silently discard missing or replaced members.

Before the trust informer synchronizes, cold startup may restore the last
committed generation. Recovery requires the same Node name, namespace and local
OVS chassis, a recorded Node UID, intact content digest, matching key/certificate
and an unexpired identity under its last committed trust. It does not import
legacy files or submit a CSR offline. Status reports `Restored`; this represents
the last verified binding, not a new live Node UID or a CA rotation acknowledgement.
After API synchronization, online identity and trust checks take precedence.
Older generations without the name/namespace binding need one successful online
reconciliation before they can recover offline.

This draft is still under implementation. Activation protection, disable
cleanup and compatibility migration must be completed and
verified before enabling the refactor in a supported release. A populated
OVSDB `ipsec_skb_mark` does not alone prove that a drop policy is installed.
The candidate image must prove startup/restart behavior and actual encrypted
traffic on the host kernel; package tests and rendered capabilities do not
substitute for that verification.

The built-in CA is generated in Go and stored in the controller-only
`ovn-ipsec-signer` Secret. `ovn-ipsec-ca` publishes only `cacert` on fresh
installations. During an upgrade the original root and key are copied before
publishing trust; the legacy `cakey` field is removed only when every live
controller Pod acknowledges the new format. The controller's Secret get/update
permissions are restricted to named Secrets in its namespace. This transition
does not revoke existing node access until controller compatibility is confirmed.
Rolling back to an old controller after finalization requires restoring its
supported Secret schema through a deliberate migration.

The node imports an existing legacy certificate/key pair only from referenced
files in its key directory, after checking the key, chassis, trust and expiry.
Requests also include the current Pod UID and signing parameters in their name,
so a rebuilt Pod can retain its pending private key without waiting on a CSR
authenticated as a deleted Pod. Storage rejects symlink entries. Reconciliation
reuses the reconnecting native OVSDB client, and probes check responsive local
IKE/monitor endpoints and a bounded reconciliation heartbeat.

The IPsec switch no longer changes the UID of OVS, the controller, or other ordinary containers. The agent runs as UID 0 with GID 65534 to access the non-root OVSDB socket without DAC capabilities. The shell installer retains its existing debug-wrapper UID/GID 0 exception for the shared socket. CNI DaemonSet rollouts explicitly use `maxSurge: 0` and `maxUnavailable: 1`.

The normal base-image build now applies the OVN output-protection extension.
The existing x86 workflow builds `Dockerfile.base` with the IPsec source patches
through the repository's existing base-image build path. The main Dockerfile
adds the IPsec agent binary and retains the existing `BASE_TAG` image selection.
Its image job verifies the packaged monitor. The existing IPsec E2E job runs the
runtime, traffic, installer, protection and Release contracts against that built
image. The cert-manager E2E job also runs the bound-identity API-signing contracts;
there is no branch-specific validation workflow. The agent checks the explicit
output-protection capability marker at startup. Unpatched images reject startup before issuing an
identity or allocating a lease. The extension generates `egress_pkt_mark`,
`ipsec_mark_out`, and `ipsec_reqid` from the node-local lease's external IDs.
The output mark remains configured when the SB IPsec switch is off.

The production Agent arms and reads back both address-family guards before
publishing the lease. A synchronous, compare-and-swap OVSDB transaction marks
existing OVN-owned tunnels and publishes the external IDs together; bridge
membership, Port ownership and Interface options are guarded against concurrent
changes. Foreign leases or protection options are rejected without replacement.
Other maps and unrelated IPsec interfaces remain untouched. Readiness now also
requires a fresh readback of the kernel guards, lease and owned tunnel options.

Initial global enable is coordinated through the controller-owned public
`ovn-ipsec-coordination` ConfigMap. The controller freezes the CNI DaemonSet
UID/template digest, public trust digest and all matching Node UIDs, including
offline nodes. Required node affinity and selectors select the initial cohort.
Prepare and Arm use separate random epochs; only fresh receipts from every
frozen target allow `NB_Global.ipsec=true`. Neither Pod readiness alone nor an
unsigned Node annotation can satisfy the barrier. A removed/replaced target
blocks the in-progress barrier without silently shrinking or rewriting its
cohort. A changed template or trust starts a fresh Prepare generation retaining
the original Node UIDs, after reading the current NB switch synchronously. If
NB already committed at Arm before a leader crash, recovery records Enabled and
preserves encryption during rollout. Existing receipts cannot cross the new
generation/epoch. Enabled cohorts add matching new Node UIDs with a new challenge;
those nodes independently pass the local startup gate first. Missing members
remain frozen, and same-name replacement requires explicit retirement. That
retirement workflow and actual new-node acceptance remain incomplete.

Disable persists Disabling before switching off NB. Synchronous native reads
must confirm both NB and SB are off before authorizing local cleanup and Release.
The challenge includes both database row UUIDs; replacements rotate the challenge
and re-enablement revokes it. Each init proves its own guarded drain before
release; the controller waits for all released results before Disabled, rather
than imposing a simultaneous all-node drain barrier that would deadlock a
zero-surge rolling update.
The signed guarded-drain barrier checks every ordinary/init container, host
execution setting and private/shared volume against the frozen template. Only
the standard read-only service-account projection is ignored. Extra siblings,
ephemeral containers, changed host paths and token-like Secret projections
invalidate both enable and cleanup receipts.

The node now separately checks local OVSDB tunnel convergence during `Cleanup`:
the same lease/output marks must remain published, while every OVN-owned tunnel
has lost `remote_name`, `ipsec_mark_out`, and `ipsec_reqid`. A fresh synchronous
snapshot rejects a changed OVS identity, delayed local updates, or a new peer;
foreign encrypted interfaces are preserved. This read-only preflight neither
issues a Disabled receipt nor withdraws the output lease or kernel guards.

The agent signs public status claims with its existing node identity key and
references a CSR authenticated as its current bound Pod. This also binds a
reused legacy identity to a rebuilt Pod without granting controller exec or
publishing key material. The controller verifies the CSR UID, server-populated
bound Pod identity, live DaemonSet/Node ownership, certificate/profile, current
container/security template, signature, epoch and two-minute receipt lifetime.
The receipt confirms current local configuration and guard readbacks; it does
not prove every peer SA or completion of CA root withdrawal. It permits at most
five seconds of future clock skew. During initial Prepare the receipt proves
an unexpired validated identity; Arm additionally requires applied runtime and
live protection. A Prepare receipt cannot be replayed at Arm. Prepared readiness
lets the zero-surge CNI rollout finish while certificate signing still uses the
ordinary Pod network. Legacy clusters already enabled retain their protection
and NB switch throughout rolling takeover.

The OVS startup script starts OVSDB first, then obtains a live readback from the
root-owned public Unix endpoint. It accepts either armed guards for the current
OVS UUID, or the controller's initial Prepare for this live Node UID when no
private reservation, public required intent or OVS output lease requires
protection. Serving this endpoint alone does not create required intent. Arm
persists the requirement before installing guards and publishing output marks.
A recreated OVSDB, API outage or a new Prepare cannot relax that requirement.
OVS mounts the public endpoint directory read-only; private identity and status
storage remain separate. The probe checks the server's Unix peer UID and never accepts a ready file.

The extension also marks flow-based and EVPN outputs, including existing ports.
These upstream paths lack the per-peer identity needed by this runtime, so this
marking blocks unsupported output while armed and does not add encrypted
connectivity support. The isolated failure harness deliberately changes these
modes only after stopping the owner to check the extension's behavior.
The production agent now checks a synchronous OVSDB snapshot before both
online activation and offline restoration. It rejects configured flow-based or
EVPN tunnels, leftover ports carrying those OVN markers, unsupported encapsulation
types, hardware offload and a userspace integration bridge. Chassis-specific
external ID overrides follow OVN precedence; unrelated chassis settings and
ordinary peer ports are preserved. A later mode change degrades the agent on
reconciliation. This check does not replace persistent output protection or
prevent another privileged writer from changing the datapath between checks.

The real-overlay acceptance harness also reads the deployed CNI and IKE process
UID, capability and nice fields: CNI must have UID 65534, nice 0 and no SYS_NICE
in either its effective or bounding set, while IKE must have nice -5 and no
effective capabilities outside the three production capabilities. The matrix covers IPv4/IPv6 and Geneve/VXLAN. The harness reads production
leases rather than allocating or overwriting a synthetic lease. The new Agent
and startup-gate paths still require a passing candidate CI run; earlier
source-prototype evidence does not validate the integrated production path.

Startup now rejects occupied IKE ports or a locked legacy OVS monitor pidfile
before changing shared certificate paths or submitting requests. The legacy
check reads the actual upstream POSIX lock; PID text alone never authorizes
a takeover or process termination. A stopped legacy monitor's stale, unlocked
pidfile is harmless. Coordinated legacy service migration remains separate.

Certificate storage retains the current, pending and previous generation, plus
every generation referenced by local OVSDB. The previous pointer is persisted
before activation and is not advanced by steady reconciliation. After the owned
runtime acknowledges the current configuration, collection removes only module
directories with valid metadata and known regular files, at least 24 hours after
the last file change. Legacy directories, unknown files and symlink targets are
never reclaimed. This bounds retired local key storage; it is not SA cleanup or
an automatic rollback policy.

Kubernetes' standard CSR cleaner owns signing-request retention (issued/denied
requests after one hour, pending requests after 24 hours, and expired issued
certificates). Cert-manager requests have an exact CSR UID owner reference so
the Kubernetes garbage collector reclaims them with the parent. The API harness
checks this relationship and preservation of an unrelated issuer request. A
cluster that disables these Kubernetes controllers needs an explicit retention
policy; the node does not receive delete privileges or delete requests by name.

The kernel protection module now journals a node-local, Node-UID-bound random
mark/reqid lease and installs independent IPv4 and IPv6 outbound block guards.
Fresh reservations reject existing masked mark aliases, reqid aliases and
potentially overlapping unmarked tunnel bypass policies. Arm persists required
intent first and verifies actual kernel selectors/action/priority/mark/index. It
rechecks preempting tunnel bypass policies on every Arm, including recovery, and
recovers interrupted insertion or lost policies without replacing a conflicting
entry. Its isolated runtime probe exercises both IP families and both tunnel UDP
ports. Normal exit does not delete these guards. The candidate runtime test uses
this module and checks lease recovery, Node replacement rejection, conflict
preservation and policy restoration in its isolated namespace. The production
Agent now uses the module; global Prepare/Arm/Enable coordination, durable
cluster-generation receipts and disable cleanup remain incomplete. The new
integrated runtime/gate acceptance is pending. A lease alone never authorizes deleting an SA;
connection/SA ownership and coordinated disable remain separate requirements.


After local release, the cleanup init retains an inactive private reservation
and owned history, publishes its released receipt and exits. It no longer
refreshes a receipt through a resident process.


The cert-manager Kind fixture uses a dedicated RSA IPsec CA generated as pure
PEM with explicit critical CA constraints and certificate/CRL signing usage.
The identical public certificate feeds the issuer and the node trust Secret.
It does not reuse ovs-pki output, whose text prefix and implicit signing usage
are incompatible with strict IPsec validation. Fixture private files stay in
an owner-only temporary directory and are removed after Secret creation.
The IPsec Kind target first installs Kube-OVN with IPsec disabled, applies the
unmodified cert-manager manifest, waits for all three deployments and the
ClusterIssuer to be Ready, and then enables IPsec. The API contract test also
uses the unmodified manifest on the ordinary Pod network.
