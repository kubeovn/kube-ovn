# network-policy-api v0.3.0

Kube-OVN uses the `v1beta1` `ClusterNetworkPolicy` API from network-policy-api
v0.3.0. CNP ingress and egress rules express transport matching through
`protocols`, for example:

```yaml
protocols:
  - tcp:
      destinationPort:
        number: 443
  - udp:
      destinationPort:
        range:
          start: 8000
          end: 9000
```

TCP, UDP and SCTP numeric ports and ranges are supported. `destinationNamedPort`
resolves against the selected destination Pods and supports all protocols and
port numbers exposed by that name. Named ports must target Pods or Namespaces;
Node, Network and domain-name peers do not provide named-port resolution.
Omitting `protocols` leaves the rule unrestricted by transport or destination
port. Domain-name peers require the DNS resolver and an `Accept` action.

Existing CNP manifests must be converted from `ports` to `protocols` before
using the v0.3.0 controller and CRD. This release does not provide automatic
object migration, legacy-format compatibility or reverse migration.

The v0.3.0 CRD serves both `v1alpha2` and `v1beta1`, with `v1beta1` as the
storage version. Both versions use the same schema and require no conversion
webhook. Existing `v1alpha2` objects remain readable and writable through
`v1beta1`; changing the CRD alone does not rewrite stored objects or convert
legacy `ports` fields to `protocols`.

The experimental CNP CRD download is pinned by `CNP_CRD_VERSION` in the
Makefile. Renovate tracks upstream releases and proposes updates for review.
ANP and BANP continue to use their independent v1alpha1 CRDs pinned to
`v0.1.7`; network-policy-api v0.3.0 only publishes the CNP conformance profile.
