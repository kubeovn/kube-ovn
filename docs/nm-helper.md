# NetworkManager helper

Before a ProviderNetwork moves a host NIC into its OVS bridge, `cni-server` sets
the NIC unmanaged in NetworkManager, so that NetworkManager does not restore the
addresses and routes that move to the bridge. The step is one D-Bus write of the
device's `Managed` property. NetworkManager authorizes that write without polkit
only for UID 0. Since v1.17, `cni-server` runs as UID 65534, so with the default
polkit policy NetworkManager rejects the write with
`org.freedesktop.NetworkManager.PermissionDenied` and the ProviderNetwork fails.

The optional `nm-helper` container does this one write as root. `cni-server`
keeps the rest of the NetworkManager logic: device lookup, the checks that can
skip the write, and the IP configuration signals that trigger a new transfer. It
sends only the final write to the helper.

The legacy chart's `func.ENABLE_NM_HELPER`, the v2 chart's
`features.enableNmHelper`, and the installer's `ENABLE_NM_HELPER` control the
daemon flag `--enable-nm-helper`, the helper container, the socket mounts and
the socket volume. All three default to `false`. When the helper is disabled,
`cni-server` writes to NetworkManager directly, as before. If NetworkManager
denies that write, the error names the NIC and both fixes: enable the helper, or
mark the NIC unmanaged on the host, for example with
`unmanaged-devices=interface-name:<nic>` in the `[keyfile]` section of a file in
`/etc/NetworkManager/conf.d/`. When the helper is enabled, `cni-server` never
falls back to the direct write. A failed request fails the ProviderNetwork
setup, and the ProviderNetwork handler retries it.

The helper is the `kube-ovn-nm-helper` binary from the same image. It runs as
UID 0 with primary GID 65534, is not privileged, keeps
`allowPrivilegeEscalation: false` and drops all container capabilities. The Pod
applies the `RuntimeDefault` seccomp profile. The binary has no file
capabilities, and the image build checks that `getcap` reports none. The helper
mounts the host's `/var/run/dbus` read-only with `HostToContainer` propagation.
Connecting to a Unix socket does not need a writable mount. NetworkManager
checks only the caller's UID, so capabilities would not help here, and none are
granted. The helper looks up the NetworkManager device for each request, so it
needs no restart after NetworkManager restarts.

The Unix socket `/run/kube-ovn-nm-helper/nm-helper.sock` is in the Pod-local
`emptyDir` volume `nm-helper-socket`, which only `cni-server` and `nm-helper`
mount. The helper creates the socket with mode `0660`, owned by UID 0 and GID
65534, and accepts only peers whose `SO_PEERCRED` UID is 0 or 65534. Each
connection carries one request of at most 4096 bytes and has a 10-second
deadline. The helper serves at most 64 requests at a time.

A request is a JSON object with one field, `Interface`. The helper applies the
kernel's basic interface name checks: not empty, at most 15 bytes, not `.` or
`..`, and with no `/`, `:` or whitespace. It then looks up the NetworkManager
device by interface name and sets `Managed` to `false`. The request has no
operation field. The helper never sets `Managed` to `true`, and nothing else in
Kube-OVN does.

The helper does not check that the interface belongs to a ProviderNetwork.
`cni-server` sends the request before the NIC is an OVS port, so OVS cannot
confirm the ownership. Any process that can reach the socket as UID 0 or 65534
can set any NetworkManager device on the node unmanaged. Only `cni-server` and
the helper mount the socket volume. Still, a process on the node that runs as
UID 65534 and GID 65534 can reach the socket through `/proc/<pid>/root` of the
`start-cniserver.sh` shell in `cni-server`, which runs with those IDs and is
dumpable, unless an LSM policy blocks it. The TProxy helper socket has the same
exposure. The helper is also root on the host system bus, and polkit authorizes
every action for UID 0. The helper code makes only the one call, but an attacker
who controls the helper process gets root on the host, for example by asking
systemd to start a unit. The image installs the helper binary with mode `0111`,
so the kernel makes the helper non-dumpable when it starts, and the helper also
calls `prctl(PR_SET_DUMPABLE, 0)`. Other processes with UID 0 and GID 65534 in
the Pod, such as the `ipsec` container, therefore cannot reach its host D-Bus
mount or its socket through `/proc/<pid>/root`. This relies on
`fs.suid_dumpable` not being 1.

Enabling the helper by changing only the daemon flag is not enough. The helper
container and its socket volume must also be present, so use the switch. There
is no container startup order. While the helper starts, requests from
`cni-server` fail and are retried. `python3 hack/test-nm-helper-container.py`
checks the enabled and disabled rendering of both charts and the installer
without applying any cluster resources. CI has no NetworkManager, so only a
manual test on a node where NetworkManager manages the NIC exercises the write.
