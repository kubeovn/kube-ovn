# Services served by a VPC NAT gateway (nft share DNAT)

A `vpc-nat-gw` can act as the load balancer of a Kubernetes Service: the controller reads the
Service and its EndpointSlices and programs the gateway's nftables directly, the same way
kube-proxy programs a node. The gateway serves the Service through its EIP (for a `LoadBalancer`
Service) and through its ClusterIP (for traffic inside the VPC).

The feature is disabled by default.

## Design in one paragraph

**The Service is the only trigger and the only writer.** A `LoadBalancer` or `ClusterIP` Service
already carries everything the gateway needs, so Service and EndpointSlice events are the only
inputs: the controller derives each `(VIP, external port, protocol)` identity and its complete
backend set, then writes the gateway nft map, the hairpin SNAT rule, the ClusterIP held on `lo`
and the VPC policy route that sends VIP traffic to the gateway.

`IptablesDnatRule` objects of `type: share` are **accounting records only**. They let an operator
query the programmed forwarding in one layer and keep EIP accounting working. Creating, editing
or deleting such an object never changes the data plane, and an operator cannot create one: the
webhook reserves `type: share` for the controller.

## Enabling it

Two independent gates on `kube-ovn-controller`:

| Flag | What it serves | Default |
| --- | --- | --- |
| `--enable-gw-nftable-lb-svc` | the ingress address of a `LoadBalancer` Service, taken from its EIP | `false` |
| `--enable-gw-nftable-svc-cluster-ip` | the ClusterIP of a Service, held on `lo` in the gateway | `false` |

Combination rules, enforced at startup:

- `--enable-gw-nftable-svc-cluster-ip` is mutually exclusive with `--enable-lb`: both own the
  ClusterIP data plane.
- `--enable-gw-nftable-lb-svc` is mutually exclusive with `--enable-lb-svc`: both own the ingress
  IP of a `LoadBalancer` Service.
- `--enable-gw-nftable-lb-svc` and `--enable-lb` combine: the gateway serves the ingress address,
  the OVN load balancer keeps serving the ClusterIP.
- `--enable-lb-svc` needs a ClusterIP data plane behind it (`--enable-lb` or
  `--enable-gw-nftable-svc-cluster-ip`), because its Pod DNATs the external address to the
  ClusterIP. The controller warns when neither is on.

Helm/`install.sh` variables: `ENABLE_GW_NFTABLE_LB_SVC`, `ENABLE_GW_NFTABLE_SVC_CLUSTER_IP`.

## Using it

Annotate the Service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: web
  annotations:
    ovn.kubernetes.io/vpc_nat_gw: gw1   # required: the gateway that serves this Service
    ovn.kubernetes.io/eip: eip1         # LoadBalancer only: the EIP that supplies the ingress IP
spec:
  type: LoadBalancer
  selector:
    app: web
  ports:
    - port: 80
      targetPort: 8080
      protocol: TCP
```

The controller publishes the EIP address in `status.loadBalancer.ingress` once the gateway is
programmed. A `ClusterIP` Service needs the gateway annotation only.

Inspect what was programmed:

```bash
# the accounting records of one Service
kubectl get iptables-dnat-rules -l ovn.kubernetes.io/nftable-lb-svc-name=web

# the identities in the gateway
kubectl -n kube-system exec <vpc-nat-gw-pod> -- nft list table ip kube-ovn
# the ClusterIPs the gateway holds
kubectl -n kube-system exec <vpc-nat-gw-pod> -- ip -4 addr show dev lo label lo:ko-vip
# the VPC routes of the VIPs
kubectl ko nbctl lr-policy-list <vpc>
```

## Rules an operator has to know

- **One EIP:port:protocol identity has one Service.** A second `LoadBalancer` Service declaring
  the same EIP and port is rejected by the webhook; if one slips past admission, the controller
  gives the identity to the deterministic winner (the lexicographically smaller Service key) and
  the loser publishes nothing and is retried.
- **The EIP must belong to the annotated gateway.** The webhook rejects a mismatching
  `IptablesEIP.spec.natGwDp` when the Service is created or its forwarding intent changes.
  The EIP does not have to be Ready or exist yet at admission; the controller checks its
  availability and readiness before programming.
- **The binding is immutable, opting out is not.** The gateway and the EIP of a Service cannot be
  changed; delete and recreate it instead. Clearing the annotations is allowed and is the
  supported way to leave the mode: the controller releases the identities, the lo addresses, the
  VIP routes, the records and finally the Service finalizer (`kubeovn.io/kube-ovn-controller`).
- **An EIP in use cannot be deleted.** The accounting records keep the EIP in use; delete the
  Service first. The two controllers are otherwise independent: an EIP event never reconciles a
  Service.
- **Deleting the gateway releases its Services.** They drop their records, their published
  ingress IP and their finalizer. The webhook rejects deleting a gateway that still has EIPs or
  ClusterIP records on it, so this path is normally reached only when the webhook is not
  installed.
- **A Service may not publish its ingress IP twice**: the gateway annotation and
  `ovn.kubernetes.io/service_external_ip_from_subnet` are mutually exclusive.

## Scope and limitations

- **Cluster traffic policy only.** All Ready endpoints are load balanced, regardless of node
  locality. `ExternalTrafficPolicy=Local`, `InternalTrafficPolicy=Local`, topology-aware routing
  and terminating-endpoint fallback are not implemented: they describe node-local semantics, and
  a vpc-nat-gw is not per-node. Client source IP is therefore not preserved.
- **IPv4 and TCP/UDP only.** The share DNAT rules match on `ip daddr`/`ip saddr`; an IPv6-only
  ClusterIP or EIP is skipped, as are ports with other protocols (SCTP).
- **Session affinity**: `ClientIP` is supported and programmed with the kube-proxy nftables
  affinity pattern. It applies to the whole identity.
- **Backends are observed through EndpointSlice only.** Upstream recomputes a slice when a Pod's
  readiness, addresses, labels, hostname or deletion timestamp change, which covers every way a
  backend joins or leaves. An attached (multus) NIC address that changes in place on a running
  Pod is not observed; such an address is assigned once per Pod.
- **Multi-NIC backends**: the gateway DNATs to the backend NIC that sits in the gateway's VPC. A
  backend with kube-ovn NICs but none in that VPC is skipped, with a Warning event on the Service.
- **HA gateways**: every running instance is programmed, and the VIP routes list all initialized
  instances as next hops.

## Upgrade notes

- The old flag `--enable-nftable-lb-svc` (default `true`) is replaced by
  `--enable-gw-nftable-lb-svc` and `--enable-gw-nftable-svc-cluster-ip` (both default `false`).
  **There is no alias**: a controller started with the old flag fails on an unknown flag. Update
  the Deployment arguments, `install.sh` variables or Helm values before upgrading.
- `type: share` `IptablesDnatRule` objects are now controller-owned accounting records. Creating
  one by hand is rejected by the webhook, and the controller no longer programs or removes NAT
  state from such objects; Services are the only writer.
