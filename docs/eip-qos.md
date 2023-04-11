# EIP QoS

EIP QoS is a feature in Kube-OVN that allows for dynamic configuration of rate limits for both Ingress and Egress traffic on custom VPC EIPs.

## Creating a QoS Policy

To create a QoS Policy, use the following YAML configuration:

```
apiVersion: kubeovn.io/v1
kind: QoSPolicy
metadata:
  name: qos-example
spec:
  bandwidthLimitRule:
    ingressMax: "1" # Mbps
    egressMax: "1" # Mbps
```

It is allowed to limit only one direction, just like this:

```
apiVersion: kubeovn.io/v1
kind: QoSPolicy
metadata:
  name: qos-example
spec:
  bandwidthLimitRule:
    ingressMax: "1" # Mbps
```

## Enabling EIP QoS
To enable EIP QoS, use the following YAML configuration:

```
kind: IptablesEIP
apiVersion: kubeovn.io/v1
metadata:
  name: eip-random
spec:
  natGwDp: gw1
  qosPolicy: qos-example
```

You can also add or update the `.spec.qosPolicy` field to an existing EIP.

## Limitations

* After creating a QoS Policy, the bandwidth limit rules cannot be changed. If you need to set new rate limit rules for an EIP, you can update a new QoS Policy to the `IptablesEIP.spec.qosPolicy` field.
* You can only delete a QoS Policy if it is not currently in use. Therefore, before deleting a QoS Policy, you must first remove the `IptablesEIP.spec.qosPolicy` field from any associated IptablesEIP.
