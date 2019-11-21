# IPv6

Through Kube-OVN does support both protocol subnets coexist in a cluster, Kubernetes control plan now only support one protocol. So you will lost some ability like probe and  service discovery if you use a protocol other than the kubernetes control plan. We recommend you use only one same ip protocol that same with kubernetes control plan.

To enable IPv6 support you need to modify the installation yaml to specify the default subnet and node subnet cidrBlock and gateway with a ipv6 format. You can apply this [v6 version yaml](https://raw.githubusercontent.com/alauda/kube-ovn/v0.9.0/yamls/kube-ovn-ipv6.yaml) at [installation step 3](install.md#to-install) for a quick start.
