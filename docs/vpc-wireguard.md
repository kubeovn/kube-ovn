# VPC WireGuard

`VpcWireGuard` and `VpcWireGuardPeer` provide remote WireGuard access into a
Kube-OVN VPC. The server runs as a privileged StatefulSet using the same image
as VPC NAT Gateway (`ovn-vpc-nat-config` `image`). Overlay routing, IPAM, DualNIC
(Multus), DNAT, and FIP are existing kube-ovn primitives; this is not OVN-native
VPN.

Requires the rebuilt NAT-gateway image that includes `wireguard-tools` and
`/kube-ovn/wireguard.sh`.

## Objects

| Kind | Short name | Purpose |
|---|---|---|
| `VpcWireGuard` | `vpc-wg` | One VPN server in a VPC |
| `VpcWireGuardPeer` | `vpc-wg-peer` | One remote client |

Both are cluster-scoped.

The server pod sits on `spec.subnet` (LAN). Client tunnel addresses come from
`spec.clientSubnet` (must be a different overlay subnet in the same VPC). The
controller installs an OVN static route `clientCIDR → lanIP` so VPC workloads
return traffic to the tunnel.

## Exposure

`spec.exposure.type` is immutable.

**DualNIC** — extra Multus NIC on `exposure.externalSubnets` (same pattern as
VpcNatGateway). Clients dial the underlay IP on `listenPort` (default 51820/UDP).

**DNAT** — requires `exposure.eip` and `exposure.natGateway`. The controller
owns an `IptablesDnatRule` that forwards UDP `listenPort` to the server LAN IP.

**FIP** — requires `exposure.eip` and `exposure.natGateway`. The controller owns
an exclusive `IptablesFIPRule` for the server LAN IP. Clients dial the EIP on
`listenPort`.

## Keys and client config

Set `generateServerKey: true` (typical) or supply `publicKey` plus
`privateKeySecretRef`. Peers use `generateKey: true` or a client `publicKey`.

When `generateKey` is true, a Secret named `vpc-wg-peer-<peer>` is written in
the server namespace (`spec.namespace`, else kube-ovn's namespace). Copy
`wg-quick.conf` to the remote user:

```bash
kubectl get secret vpc-wg-peer-<peer> -n kube-system -o jsonpath='{.data.wg-quick\.conf}' | base64 -d
```

`status.ready` is true only after the server pod is running and `wg0` is up.

## Example

See `yamls/vpc-wireguard.yaml`.
