# Hardware VTEP Binding

Extend a Kube-OVN Subnet (OVN Logical Switch) to bare-metal servers attached to a
Hardware VTEP-capable physical switch (any switch that implements the standard
OVSDB Hardware VTEP schema, for example Cisco Nexus).

## Topology

```text
                         Kube-OVN Subnet
                          10.20.0.0/24
                                |
                +---------------+---------------+
                |                               |
           KubeVirt VM                        Pod
          10.20.0.20                       10.20.0.30
                |
          OVN Logical Switch
                |
             VXLAN
                |
        Hardware VTEP Switch
                |
          Ethernet1/20
                |
         Bare-metal Server
           10.20.0.10
```

Kube-OVN is **not** responsible for configuring the IP address inside the physical
server.

## How it works

1. You create a `VtepBinding` that references a Subnet and a Hardware VTEP
   physical switch / logical switch / port / VLAN.
2. `kube-ovn-controller` creates an OVN Northbound Logical Switch Port with
   `type=vtep` and options:
   - `vtep-physical-switch`
   - `vtep-logical-switch`
3. When `--vtep-db-addr` / `VTEP_DB_ADDR` is set, the controller also writes the
   Hardware VTEP OVSDB:
   - `Logical_Switch` (named by `spec.vtepLogicalSwitch` or the Subnet)
   - `Physical_Port.vlan_bindings[vlanID] = Logical_Switch`
4. `ovn-controller-vtep` (deployed when `ENABLE_HARDWARE_VTEP=true`) synchronizes
   OVN Southbound with the Hardware VTEP OVSDB (tunnel keys, remote MACs,
   chassis binding). `VTEP_DB_ADDR` is required in this mode: the start script
   cannot launch `ovn-controller-vtep` without a Hardware VTEP OVSDB remote.
5. `VtepBinding.status.ready` becomes true only after the SB `Port_Binding` for
   the VTEP LSP has a non-empty chassis. `Port_Binding.up=false` does not block
   Ready; `ovn-controller-vtep` can leave `up=false` after chassis assignment.

## Enable Hardware VTEP

Helm (v1 chart):

```bash
helm upgrade --install kube-ovn charts/kube-ovn -n kube-system \
  --set func.ENABLE_HARDWARE_VTEP=true \
  --set networking.VTEP_DB_ADDR='tcp:[192.0.2.10]:6640'
```

Helm (v2 chart):

```bash
helm upgrade --install kube-ovn charts/kube-ovn-v2 -n kube-system \
  --set features.enableHardwareVtep=true \
  --set networking.vtepDbAddr='tcp:[192.0.2.10]:6640'
```

`install.sh`:

```bash
ENABLE_HARDWARE_VTEP=true VTEP_DB_ADDR='tcp:[192.0.2.10]:6640' bash dist/images/install.sh
```

This deploys `ovn-controller-vtep` and configures `kube-ovn-controller` with
`--enable-hardware-vtep` and `--vtep-db-addr`. `VTEP_DB_ADDR` is required when
the feature is enabled. The switch must already expose a Hardware VTEP OVSDB with
`Physical_Switch` / `Physical_Port` rows that match the CR.

`--enable-hardware-vtep` is the feature gate (default `false`). When it is disabled,
the controller does not start the `VtepBinding` informer or worker and will not
create NB `type=vtep` ports. `--vtep-db-addr` enables Hardware VTEP OVSDB writes
after the feature is turned on.

If the Hardware VTEP DB is unreachable at controller startup, kube-ovn-controller
still starts and continues reconciling other resources. `VtepBinding.status.ready`
follows OVN Southbound chassis attachment (`Port_Binding.chassis` non-empty).
Hardware VTEP DB health is reported on the separate `VTEPDBReady` condition and
retried in the background. The controller also emits Kubernetes Events for
`WaitingForChassis`, chassis loss, VTEP DB unavailability or reconcile failure,
and cleanup success or failure, so `kubectl describe vtepbinding` explains why
`Ready` is false.

## Prerequisites

- OVN with Hardware VTEP support (Kube-OVN ships OVN 25.03; VTEP packages are
  kept in the image).
- Hardware VTEP OVSDB reachable at `VTEP_DB_ADDR` (required when Hardware VTEP
  is enabled) containing:
  - `Physical_Switch` whose name matches `spec.physicalSwitch`
  - `Physical_Port` whose name matches `spec.physicalPort` on that switch
- The referenced Subnet must already exist; the validating webhook rejects the
  CR otherwise.
- Optional: TLS materials in the `kube-ovn-tls` secret when using `ssl:` remotes.

Kube-OVN does **not** configure the switch via NX-API or CLI, and does **not**
create `Physical_Switch` / `Physical_Port` rows (those come from the switch NMS).

## Example

```yaml
apiVersion: kubeovn.io/v1
kind: Subnet
metadata:
  name: tenant-a-backend
spec:
  vpc: tenant-a
  cidrBlock: 10.20.0.0/24
  gateway: 10.20.0.1
  excludeIps:
  - 10.20.0.0..10.20.0.10
---
apiVersion: kubeovn.io/v1
kind: VtepBinding
metadata:
  name: tenant-a-rack1
spec:
  subnet: tenant-a-backend
  physicalSwitch: nexus01
  # optional; defaults to subnet name
  vtepLogicalSwitch: tenant-a-backend
  physicalPort: Ethernet1/20
  vlanID: 120
```

Then on the bare-metal host connected to `Ethernet1/20` (VLAN 120), configure an
IP in `10.20.0.0/24` (for example `10.20.0.10/24`). Pods and KubeVirt VMs on
`tenant-a-backend` should have L2 reachability to that host once
`status.ready=true`.

## Multi-tenant usage

Create one `VtepBinding` (and matching switch VLAN binding) per tenant Subnet:

| Tenant | Subnet | VTEP Logical Switch | VLAN |
|--------|--------|---------------------|------|
| A | tenant-a-backend | tenant-a-backend | 120 |
| B | tenant-b-backend | tenant-b-backend | 130 |

`(physicalSwitch, vtepLogicalSwitch)` and `(physicalSwitch, physicalPort, vlanID)`
must each be unique across `VtepBinding` resources. Spec fields other than
status are immutable after create (`subnet`, `physicalSwitch`,
`vtepLogicalSwitch`, `physicalPort`, `vlanID`); recreate the CR to change the
physical attachment.

## Limitations

- No BGP / EVPN / MLAG / vPC automation.
- No DHCP or IPAM for bare-metal servers.
