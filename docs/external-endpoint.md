# Experimental ExternalEndpoint

An `ExternalEndpoint` attaches a NIC of a workload that runs outside Kubernetes, for example a libvirt VM on a hypervisor that is not a Kubernetes node, to a Kube-OVN overlay subnet. Kube-OVN owns the address and the logical switch port of the NIC exactly as it does for a pod NIC; the hypervisor owner owns the VM and plugs the NIC in.

The feature is disabled by default. Enable it with `--enable-external-endpoint=true` on kube-ovn-controller (`ENABLE_EXTERNAL_ENDPOINT=true` for `install.sh`, `func.ENABLE_EXTERNAL_ENDPOINT` / `features.ENABLE_EXTERNAL_ENDPOINT` in the charts). Install the `external-endpoints.kubeovn.io` CRD first: the controller waits for its informer to sync.

## Example

```yaml
apiVersion: kubeovn.io/v1
kind: ExternalEndpoint
metadata:
  name: vm1-eth0
  namespace: tenant-a
spec:
  subnet: tenant-a-net          # required, immutable
  chassis: hv-01                # required: OVN chassis name or hostname of the hypervisor
  macAddress: 02:ac:10:ee:00:01 # optional, immutable, random when empty
  ipAddress: 10.77.0.50         # optional, immutable, random when empty; "v4,v6" for dual stack
  portSecurity: true            # optional, default true
  securityGroups: [vm-sg]       # optional
```

```text
$ kubectl -n tenant-a get eep
NAME       SUBNET         V4IP         V6IP   MAC                 CHASSIS   UP     AGE
vm1-eth0   tenant-a-net   10.77.0.50          02:ac:10:ee:00:01   hv-01     true   1m
```

`status.interfaceID` (defaults to `metadata.uid`) is the logical switch port name and the OVS `iface-id` the hypervisor sets. With libvirt no hook is needed:

```xml
<interface type='bridge'>
  <mac address='02:ac:10:ee:00:01'/>
  <source bridge='br-int'/>
  <virtualport type='openvswitch'><parameters interfaceid='STATUS.INTERFACEID'/></virtualport>
  <model type='virtio'/>
</interface>
```

The guest gets its address from the subnet's DHCP options when `enableDHCP` is set on the subnet, or can configure it statically from `status`.

## Behavior

- The address is allocated from the subnet IPAM like a pod address, recorded in an `IP` CR (`podType: ExternalEndpoint`, label `ovn.kubernetes.io/external_endpoint_uid`), counted in the subnet status and restored at controller start.
- The logical switch port carries the addresses, port security, the subnet's DHCP options, the security groups, `options:requested-chassis=<spec.chassis>` and `external_ids:external_endpoint=<namespace>/<name>`, written in one NB transaction. Only the requested chassis binds the port.
- Conditions: `Ready` (address and port match the spec) and `Bound` (a chassis claimed the port). The controller resyncs every `--inspect-interval` and repairs a deleted or modified port or `IP` CR.
- Deleting the endpoint deletes the port, the `IP` CR and releases the address; the NIC is cut off immediately, like a deleted pod. A subnet does not finish deleting while endpoints use it.
- The chassis an endpoint is pinned to is never garbage collected, even though it is not a Kubernetes node.
- A port or `IP` CR that already belongs to someone else is never taken over.

## Requirements and limits

- Overlay subnets only; underlay (VLAN) and MAC-only subnets are rejected (`UnsupportedSubnet`). Subnets bound to namespaces reject endpoints of other namespaces.
- The hypervisor runs OVS and `ovn-controller` against the Kube-OVN southbound database with the cluster's tunnel type, and its hostname must not be a Kubernetes node name. Protect the southbound database with SSL/RBAC: any chassis connected to it is trusted.
- Endpoints are not selected by `NetworkPolicy` pod selectors (use `ipBlock` peers) and do not use the per-node distributed gateway for north-south traffic; use a centralized gateway or a custom VPC.
- `spec.chassis` takes one chassis; changing it moves the binding.
- Disabling the feature keeps existing ports, their chassis and their addresses, but stops reconciling them; deleting an endpoint or its `IP` CR then waits until the feature is enabled again.
- Before downgrading to a version without this feature, delete all endpoints.
