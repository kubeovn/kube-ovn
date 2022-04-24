# Backgroud
There is a demand for fine-grained traffic control at present. This can be implemented with ACL rules in ovn. We provide support for detailed acl control in Kube-OVN v1.10.0. 
# Implementation
New fields are added in subnet crd, which are used for ACL. The detailed crd of subnet is as follows:
```
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: subnets.kubeovn.io
spec:
  group: kubeovn.io
  versions:
    - name: v1
      served: true
      storage: true
      subresources:
        status: {}
      additionalPrinterColumns:
      - name: Provider
        type: string
        jsonPath: .spec.provider
      - name: Vpc
        type: string
        jsonPath: .spec.vpc
      - name: Protocol
        type: string
        jsonPath: .spec.protocol
      - name: CIDR
        type: string
        jsonPath: .spec.cidrBlock
      - name: Private
        type: boolean
        jsonPath: .spec.private
      - name: NAT
        type: boolean
        jsonPath: .spec.natOutgoing
      - name: Default
        type: boolean
        jsonPath: .spec.default
      - name: GatewayType
        type: string
        jsonPath: .spec.gatewayType
      - name: V4Used
        type: number
        jsonPath: .status.v4usingIPs
      - name: V4Available
        type: number
        jsonPath: .status.v4availableIPs
      - name: V6Used
        type: number
        jsonPath: .status.v6usingIPs
      - name: V6Available
        type: number
        jsonPath: .status.v6availableIPs
      - name: ExcludeIPs
        type: string
        jsonPath: .spec.excludeIps
      schema:
        openAPIV3Schema:
          type: object
          properties:
            status:
              type: object
              properties:
                v4availableIPs:
                  type: number
                v4usingIPs:
                  type: number
                v6availableIPs:
                  type: number
                v6usingIPs:
                  type: number
                activateGateway:
                  type: string
                dhcpV4OptionsUUID:
                  type: string
                dhcpV6OptionsUUID:
                  type: string
                conditions:
                  type: array
                  items:
                    type: object
                    properties:
                      type:
                        type: string
                      status:
                        type: string
                      reason:
                        type: string
                      message:
                        type: string
                      lastUpdateTime:
                        type: string
                      lastTransitionTime:
                        type: string
            spec:
              type: object
              properties:
                vpc:
                  type: string
                default:
                  type: boolean
                protocol:
                  type: string
                  enum:
                    - IPv4
                    - IPv6
                    - Dual
                cidrBlock:
                  type: string
                namespaces:
                  type: array
                  items:
                    type: string
                gateway:
                  type: string
                provider:
                  type: string
                excludeIps:
                  type: array
                  items:
                    type: string
                vips:
                  type: array
                  items:
                    type: string
                gatewayType:
                  type: string
                allowSubnets:
                  type: array
                  items:
                    type: string
                gatewayNode:
                  type: string
                natOutgoing:
                  type: boolean
                externalEgressGateway:
                  type: string
                policyRoutingPriority:
                  type: integer
                  minimum: 1
                  maximum: 32765
                policyRoutingTableID:
                  type: integer
                  minimum: 1
                  maximum: 2147483647
                  not:
                    enum:
                      - 252 # compat
                      - 253 # default
                      - 254 # main
                      - 255 # local
                private:
                  type: boolean
                vlan:
                  type: string
                logicalGateway:
                  type: boolean
                disableGatewayCheck:
                  type: boolean
                disableInterConnection:
                  type: boolean
                enableDHCP:
                  type: boolean
                dhcpV4Options:
                  type: string
                dhcpV6Options:
                  type: string
                enableIPv6RA:
                  type: boolean
                ipv6RAConfigs:
                  type: string
                htbqos:
                  type: string
                acls:                                 // The parameters used for acl rules
                  type: array
                  items:
                    type: object
                    properties:
                      direction:
                        type: string
                        enum:
                          - from-lport
                          - to-lport
                      priority:
                        type: integer
                        minimum: 0
                        maximum: 32767
                      match:
                        type: string
                      action:
                        type: string
                        enum:
                          - allow-related
                          - allow-stateless
                          - allow
                          - drop
                          - reject
  scope: Cluster
  names:
    plural: subnets
    singular: subnet
    kind: Subnet
    shortNames:
      - subnet
---
```
The newly added acls field is an array parameter. The object contains parameters that must be configured for an acl.

The `match` field should be configured carefully, which can be referenced in [Logical_Flow_TABLE](https://www.mankier.com/5/ovn-sb#Logical_Flow_TABLE).

# Examples
1. Create subnet and test pods

Create subnets and pods assigned IPs from different subnets.
The pod in the default namespace is assigned IP from the ovn-default subnet.
The pod in the test namespace is assigned IP from the private subnet.

```
apple@bogon ovn-test % kubectl get subnet
NAME          PROVIDER   VPC           PROTOCOL   CIDR            PRIVATE   NAT     DEFAULT   GATEWAYTYPE   V4USED   V4AVAILABLE   V6USED   V6AVAILABLE   EXCLUDEIPS
join          ovn        ovn-cluster   IPv4       100.64.0.0/16   false     false   false     distributed   2        65531         0        0             ["100.64.0.1"]
ovn-default   ovn        ovn-cluster   IPv4       10.16.0.0/16    false     true    true      distributed   6        65527         0        0             ["10.16.0.1"]
private       ovn        ovn-cluster   IPv4       2.2.0.0/16      true      true    false     distributed   2        65531         0        0             ["2.2.0.1"]
apple@bogon ovn-test % kubectl get pod -o wide
NAME                       READY   STATUS    RESTARTS   AGE   IP           NODE              NOMINATED NODE   READINESS GATES
dynamic-7d8d7874f5-fxhrq   1/1     Running   0          22h   10.16.0.12   kube-ovn-worker   <none>           <none>
apple@bogon ovn-test %
apple@bogon ovn-test % kubectl get pod -o wide -n test
NAME                       READY   STATUS    RESTARTS   AGE     IP        NODE                     NOMINATED NODE   READINESS GATES
dynamic-7d8d7874f5-8v4jd   1/1     Running   0          4h42m   2.2.0.3   kube-ovn-control-plane   <none>           <none>
dynamic-7d8d7874f5-fcccl   1/1     Running   0          4h42m   2.2.0.2   kube-ovn-worker          <none>           <none>
apple@bogon ovn-test %
```

Edit private subnet and add ACL parameters
```
spec:
  acls:                                                 // ACL parameters
  - action: reject
    direction: to-lport
    match: ip4.src==10.16.0.12 && ip4.dst==2.2.0.3
    priority: 2022
  - action: allow
    direction: to-lport
    match: ip4.src==10.16.0.12 && ip4.dst==2.2.0.2
    priority: 2222
  ...
```

The acl rules in OVN NB DB show as follows
```
root@kube-ovn-control-plane:/kube-ovn# ovn-nbctl acl-list  private
  to-lport  2222 (ip4.src==10.16.0.12 && ip4.dst==2.2.0.2) allow
  to-lport  2022 (ip4.src==10.16.0.12 && ip4.dst==2.2.0.3) reject
root@kube-ovn-control-plane:/kube-ovn#
```
### ACL test

Use the pod in the default namespace as a test pod, test connection to pods in the test namespace.
```
apple@bogon ovn-test % kubectl get pod -o wide
kubectNAME                       READY   STATUS    RESTARTS   AGE   IP           NODE              NOMINATED NODE   READINESS GATES
dynamic-7d8d7874f5-fxhrq   1/1     Running   0          22h   10.16.0.12   kube-ovn-worker   <none>           <none>
apple@bogon ovn-test % kubectl get pod -o wide -n test
NAME                       READY   STATUS    RESTARTS   AGE     IP        NODE                     NOMINATED NODE   READINESS GATES
dynamic-7d8d7874f5-8v4jd   1/1     Running   0          4h50m   2.2.0.3   kube-ovn-control-plane   <none>           <none>
dynamic-7d8d7874f5-fcccl   1/1     Running   0          4h50m   2.2.0.2   kube-ovn-worker          <none>           <none>
apple@bogon ovn-test %
apple@bogon ovn-test % kubectl exec -it dynamic-7d8d7874f5-fxhrq -- bash
bash-5.0# ping -c 3 2.2.0.3
PING 2.2.0.3 (2.2.0.3): 56 data bytes

--- 2.2.0.3 ping statistics ---
3 packets transmitted, 0 packets received, 100% packet loss
bash-5.0#
bash-5.0# ping -c 3 2.2.0.2
PING 2.2.0.2 (2.2.0.2): 56 data bytes
64 bytes from 2.2.0.2: seq=0 ttl=63 time=1.924 ms
64 bytes from 2.2.0.2: seq=1 ttl=63 time=0.477 ms
64 bytes from 2.2.0.2: seq=2 ttl=63 time=0.154 ms

--- 2.2.0.2 ping statistics ---
3 packets transmitted, 3 packets received, 0% packet loss
round-trip min/avg/max = 0.154/0.851/1.924 ms
bash-5.0#
```
According to the result, the pod in the default namespace can connect to one pod in the test namespace, but can not connect to the other pod in the test namespace. The result is consistent with the ACL configuration.