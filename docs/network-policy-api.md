# network-policy-api v0.2.0

Kube-OVN uses the v0.2.0 `ClusterNetworkPolicy` API. CNP ingress and egress
rules express transport matching through `protocols`, for example:

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

TCP, UDP and SCTP numeric ports and ranges are supported. Omitting `protocols`
leaves the rule unrestricted by transport or destination port. Named ports
are rejected before applying OVN changes. Domain-name peers require the DNS
resolver and an `Accept` action.

Existing CNP manifests must be converted from `ports` to `protocols` before
using the v0.2.0 controller and CRD. This release does not provide automatic
object migration, legacy-format compatibility or reverse migration. The
v1alpha2 API version stays the same, so changing the CRD alone does not convert
stored policy specifications.

The experimental CNP CRD download is pinned by `CNP_CRD_VERSION` in the
Makefile. Renovate tracks upstream releases and proposes updates for review.
ANP and BANP continue to use their independent v1alpha1 CRDs and conformance
test module.
