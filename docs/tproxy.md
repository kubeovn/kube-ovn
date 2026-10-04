# TProxy namespace helper

Custom VPC probes use the node daemon's transparent TCP listener. The daemon
discovers target Pods, keeps probe state, manages interception rules and routes,
and forwards streams. A separate `tproxy` container opens backend TCP connections
inside the destination Pod's network namespace and returns connected file
descriptors through `SCM_RIGHTS`. The daemon's TProxy path no longer calls `setns`.

The legacy chart's `func.ENABLE_TPROXY`, the v2 chart's `features.enableTproxy`,
and the installer's `ENABLE_TPROXY` control both the daemon flag and the helper.
When disabled, the helper container, shared socket mounts and socket volume are
absent. The daemon retains route/rule cleanup so disabling the feature does not
depend on a container that has already been removed.

The helper is a standalone binary from the same image. It runs as root with the
nobody service group, drops all capabilities except `SYS_ADMIN`, and disables
privilege escalation. OVSDB and runtime namespace directories are mounted
read-only; netns mounts use `HostToContainer` propagation. The group permits
access to the OVSDB socket owned by the unprivileged OVS service. The helper
binary has no file capability and is not launched or supervised by OVS.

The Unix socket `/run/kube-ovn-tproxy/tproxy.sock` resides in a Pod-local
`emptyDir` mounted only by the daemon and helper. Requests must come from root
or the nobody service user. The helper validates the namespace path and TCP
destination, then synchronously checks that an OVS interface records the target
Pod IP, namespace and Kube-OVN ownership. It does not monitor or cache all node
interfaces. Its OVSDB connection reconnects after a database restart.

Namespace TCP dials have a three-second timeout. Successful probes close their
returned sockets immediately. Forwarded sockets remain owned by the daemon,
including across helper restarts. kubelet restarts the helper independently;
startup or temporary helper failures are failed dials retried by later probes.
There is no container startup order requirement.

Enabling TProxy by manually changing only a daemon argument is insufficient:
the helper container and its shared socket volume must also be present. Upgrade
the image and Pod template together. The custom VPC probe E2E covers this setup,
while `python3 hack/test-tproxy-container.py` checks enabled/disabled rendering
for both charts, DPDK and the installer without applying any cluster resources.

This refactor is a prerequisite for CNI privilege separation in PR #7593.
Other daemon paths still require their existing capabilities; this change alone
does not remove `SYS_ADMIN` from the main CNI container or daemon binary.
