# CNI privilege separation

Kube-OVN now supports a two-phase CNI path. The daemon resolves Kubernetes state and returns a typed network plan. The CNI binary, started by the container runtime, applies the host and Pod network changes and then commits the observed result to the daemon.

```text
CNI ADD -> daemon: prepare plan
CNI ADD -> local executor: veth/VF, OVS, netns, routes, QoS
CNI ADD -> daemon: commit observed result
```

The plan contains network intent and validated resource identifiers. It does not contain shell commands, arbitrary paths, OVS transactions, or a namespace path chosen by the daemon. The executor validates the plan again and uses the runtime-provided network namespace for the operation.

The same sequence is used for DEL. DEL obtains a deletion plan before removing an interface, so a missing Pod or a missing namespace does not make the executor guess which host object to remove. The local executor owns host networking cleanup; the daemon commit handles Kubernetes and egress bookkeeping.

The daemon rejects old daemon-side ADD and DEL execution unconditionally when `PrepareOnly` is false. The `install-cni` initContainer installs the new CNI binary before `cni-server` starts, so there is no compatibility switch that can re-enable daemon-side host networking.

The CNI server runs with the chart's unprivileged service user (root is retained when IPsec requires it), keeps the capabilities needed by its continuous node work, and does not receive the legacy CNI `SYS_ADMIN` or `SYS_PTRACE` capabilities. This change moves Pod attachment privileges out of the `kube-ovn-daemon` CNI server; node gateway reconciliation, ProviderNetwork updates, netfilter, IPsec, TProxy, and other continuous node work remain in the node daemon until their own execution mode is migrated.

## Upgrade and rollback

The installer publishes `kube-ovn` as an atomic symlink to an immutable execution
bundle under the CNI bin directory. The statically linked CNI uses libovsdb to
access `/run/openvswitch/db.sock` and ethtool ioctls in the current network
namespace. It does not execute external binaries or scripts, and needs no host
shell, tool packages, ELF loader, or shared libraries. Installation checks the
CNI VERSION command before publishing the bundle. Previous bundles are retained
for running invocations and rollback; operators may retire them after those
invocations have exited and the rollback window has closed.

1. Install the new image and CNI binary on a node.
2. Verify the CNI binary can reach the daemon socket and that a test ADD receives a plan.
3. Keep the new daemon and CNI binary paired during rollout; the installer writes the binary before starting `cni-server` on each node.
4. During rollback, restore the matching older daemon and CNI binary together. Existing new-path attachments must be deleted by the new binary or by an explicit cleanup operation before the old binary becomes the sole owner.

The v1 request and response types remain wire-compatible, but requests that omit `prepare_only` or set it to false are rejected with HTTP 426 so old clients fail immediately instead of reaching a privileged operation.
