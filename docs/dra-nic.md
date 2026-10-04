# DRA NICs (experimental)

Secondary pod NICs can be requested through [Dynamic Resource Allocation](https://kubernetes.io/docs/concepts/scheduling-eviction/dynamic-resource-allocation/) (DRA) instead of a Multus network attachment. A DRA driver publishes Kube-OVN subnets as devices, the scheduler allocates them to a pod's `ResourceClaim`, and kube-ovn-controller treats every allocated device like a Multus attachment: it allocates the address and creates the logical switch port, ip CR and pod annotations through the regular pod allocation path.

The feature is experimental and disabled by default. The contract below may still change.

## Enabling

- kube-ovn-controller flag `--enable-dra-nic` (`ENABLE_DRA_NIC` in `install.sh` and the legacy chart, `features.enableDraNic` in the v2 chart)
- `--dra-nic-driver-name` selects the DRA driver whose devices are NICs, default `nic.kubeovn.io`
- Requires the `resource.k8s.io/v1` API (Kubernetes 1.34 or later). The controller only watches ResourceClaims and ResourceSlices when the feature is enabled; on a cluster that does not serve them it logs a warning and runs without DRA NIC support.

## Driver contract

A device is a Kube-OVN NIC when it is allocated by the configured driver. kube-ovn-controller reads:

- the device attribute `subnetName` (`<driver>/subnetName` or unqualified) from the newest ResourceSlice generation of the device's pool; it names the subnet to allocate from. While not all slices of that generation (`resourceSliceCount`) are known, the pod is retried; slice changes requeue the pods of the pool's claims.
- the subnet's `spec.provider`, which must be a dedicated provider (for example `<name>.<namespace>.ovn`, the convention of a NetworkAttachmentDefinition). The default provider `ovn` belongs to the pod's primary NIC and is rejected, and so is a non-OVN provider: the NIC is bound to a logical switch port, which only an OVN subnet has.
- `interfaceName` from the driver's opaque claim config that applies to the request; a config for a `firstAvailable` request applies to the subrequest it was allocated by (`resourceclaim.ConfigForResult`). It keys several NICs on one provider and must not be `eth0` or an interface of a Multus attachment (`net<index>` unless the selection requests a name). Without one the driver's default `net1` is assumed.

kube-ovn-controller records the subnet and provider of each NIC on its claim when it first resolves it, in the annotation `ovn.kubernetes.io/dra_providers` (JSON keyed by the allocated device: `{"<pool>/<device>": {"subnet": ..., "provider": ...}}`), and uses the record from then on. A later change of the subnet's `spec.provider` or of the published device therefore does not move an allocated NIC: running pods, and the pods sharing a claim during a live migration, keep it; only NICs of new claims use the new provider. A driver should read the provider from the record when it is there.

Each NIC is keyed by a provider key, which is the subnet's provider, or `<provider>.<interfaceName>` when several NICs of the pod share the provider (the same rule Kube-OVN uses for repeated attachments of one NAD). A DRA NIC must not use the provider of a Multus attachment of the same pod.

Once the pod is bound to a node and all its claims are allocated, kube-ovn-controller

- creates the logical switch port and ip CR `<name>.<namespace>.<provider key>`, where `<name>` is the pod name, or the VM name for KubeVirt VM pods with keep-vm-ip
- writes the pod annotations `<provider key>.kubernetes.io/{ip_address,mac_address,cidr,gateway,logical_switch,allocated}`, and `virtualmachine` when the port is named after a VM. Dual-stack values are comma separated.

The driver is expected to

1. publish the subnets as devices with the `subnetName` attribute
2. in `NodePrepareResources`, wait for `<provider key>.kubernetes.io/allocated: "true"` and read the address annotations
3. plug the interface into the pod network namespace and attach its host end to `br-int` with `external_ids:iface-id=<logical switch port name>`
4. not release anything on unprepare: kube-ovn-controller releases the address when the pod is deleted, or keeps it for a keep-vm-ip VM, as for Multus attachments

Ports the driver leaves behind are cleaned up by kube-ovn-cni: its periodic garbage collection deletes OVS interfaces that carry the `external_ids` `pod_name` and `pod_namespace` and whose link is not up, once the named pod no longer exists on the node (kube-ovn-cni itself puts the VM's name there for the pod of a KubeVirt VM, and the collection resolves such a name through the labels of the live virt-launcher pod; the reference driver puts the pod's own name there, which the collection handles as well). A driver that sets these keys on the ports it adds gets leftovers removed that way; a driver that does not has to remove them itself. The reference driver sets `iface-id`, `pod_name`, `pod_namespace`, `pod_netns` and `ip`. This is a contract with kube-ovn-cni, so a change to those keys or to that garbage collection affects drivers.

## Shared claims

A claim stands for one NIC with one address, so it can only be shared by the virt-launcher pods of one KubeVirt VM: the source and the target pod of a live migration, which keep the VM's address. A claim shared with any other pod is rejected. A VM whose NIC comes from a `ResourceClaimTemplate` gets a second claim for the migration target, which the scheduler counts against the pool although the VM keeps its address; with a capacity-limited pool, a named `ResourceClaim` per VM lets the migration run while the pool is full. KubeVirt allows that up to v1.10.0-beta.0; from v1.10.0-rc.0 on its admission refuses to migrate a VM whose DRA network uses a named claim ([kubevirt/kubevirt#19468](https://github.com/kubevirt/kubevirt/pull/19468)), so such a VM then needs a claim template and a free address in the pool.

## Lifecycle

- Pod deletion releases the NIC's port, ip CR and address, also when the claim's reservation is already gone.
- KubeVirt VMs with keep-vm-ip keep the IP and MAC of their DRA NICs across restarts and while stopped, like their Multus attachments.

## Implementations

- [kube-ovn-dra-driver](https://github.com/soer3n/kube-ovn-dra-driver) is a reference driver that plugs the NICs through an NRI hook. For KubeVirt VMs it adds a bridge and tap and serves DHCP, or leaves DHCP to OVN when the subnet sets `enableDHCP`; it also ships a KubeVirt `Plugin` (Plugins API, KubeVirt v1.9 and later) that adds the NIC to the VM's domain, since KubeVirt's built-in bindings do not resolve DRA networks yet.
- [kube-ovn-network-binding-plugin](https://github.com/soer3n/kube-ovn-network-binding-plugin) does the same as a binding sidecar, for clusters where KubeVirt's `Plugins` feature gate cannot be enabled.

## Testing

`make kube-ovn-dra-e2e` runs against a cluster installed with `ENABLE_DRA_NIC=true` and skips otherwise; the CI job `kube-ovn-dra-e2e` runs it that way. It covers only the kube-ovn-controller side: it publishes a static ResourceSlice, so no driver is needed, and the pod never starts, so no nic is plugged.
