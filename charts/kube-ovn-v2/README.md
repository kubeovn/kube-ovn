# kube-ovn

![Version: 2.0.0](https://img.shields.io/badge/Version-2.0.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 1.14.0](https://img.shields.io/badge/AppVersion-1.14.0-informational?style=flat-square)

Helm chart for Kube-OVN

## Requirements

Kubernetes: `>= 1.29.0-0`

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| agent | object | `{"annotations":{},"dpdkTunnelInterface":"br-phy","interface":"","labels":{},"metrics":{"port":10665},"mirroring":{"enabled":false,"interface":"mirror0"},"podAnnotations":{},"podLabels":{},"resources":{"limits":{"cpu":"1000m","memory":"1Gi"},"requests":{"cpu":"100m","memory":"100Mi"}}}` | Configuration for kube-ovn-cni, the agent responsible for handling CNI requests from the CRI |
| agent.annotations | object | `{}` | Annotations to be added to all top-level agent objects (resources under templates/agent) |
| agent.labels | object | `{}` | Labels to be added to all top-level agent objects (resources under templates/agent) |
| agent.metrics | object | `{"port":10665}` | Agent metrics configuration |
| agent.metrics.port | int | `10665` | Configure the port on which the agent service will serve metrics |
| agent.mirroring | object | `{"enabled":false,"interface":"mirror0"}` | Mirroring of the traffic for debug or analysis https://kubeovn.github.io/docs/stable/en/guide/mirror/ |
| agent.mirroring.enabled | bool | `false` | Enable mirroring of the traffic |
| agent.mirroring.interface | string | `"mirror0"` | Interface on which to send the mirrored traffic |
| agent.podAnnotations | object | `{}` | Annotations to be added to the agent pods (kube-ovn-cni) |
| agent.podLabels | object | `{}` | Labels to be added to the agent pods (kube-ovn-cni) |
| agent.resources | object | `{"limits":{"cpu":"1000m","memory":"1Gi"},"requests":{"cpu":"100m","memory":"100Mi"}}` | Agent daemon resource limits & requests ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| apiNad | object | `{"enabled":false,"name":"ovn-kubernetes-api","provider":"{{ .Values.apiNad.name }}.{{ .Values.namespace }}.ovn","subnet":{"cidrBlock":"100.100.0.0/16,fd00:100:100::/112","name":"ovn-kubernetes-api","protocol":"Dual"}}` | API NetworkAttachmentDefinition to give some pods (CoreDNS, NAT GW) in custom VPCs access to the K8S API This requires Multus to be installed |
| apiNad.enabled | bool | `false` | Enable the creation of the API NAD |
| apiNad.name | string | `"ovn-kubernetes-api"` | Name of the NAD |
| apiNad.provider | string | `"{{ .Values.apiNad.name }}.{{ .Values.namespace }}.ovn"` | Name of the provider, must be in the form "nadName.nadNamespace.ovn" |
| apiNad.subnet | object | `{"cidrBlock":"100.100.0.0/16,fd00:100:100::/112","name":"ovn-kubernetes-api","protocol":"Dual"}` | Subnet associated with the NAD, it will have full access to the API server |
| apiNad.subnet.cidrBlock | string | `"100.100.0.0/16,fd00:100:100::/112"` | CIDR block used by the API subnet |
| apiNad.subnet.name | string | `"ovn-kubernetes-api"` | Name of the subnet |
| apiNad.subnet.protocol | string | `"Dual"` | Protocol for the API subnet |
| central | object | `{"annotations":{},"labels":{},"ovnLeaderProbeInterval":5,"ovnNorthdNThreads":1,"ovnNorthdProbeInterval":5000,"podAnnotations":{},"podLabels":{},"resources":{"limits":{"cpu":"3","memory":"4Gi"},"requests":{"cpu":"300m","memory":"200Mi"}}}` | Configuration for ovn-central, the daemon containing the northbound/southbound DBs and northd |
| central.annotations | object | `{}` | Annotations to be added to all top-level ovn-central objects (resources under templates/central) |
| central.labels | object | `{}` | Labels to be added to all top-level ovn-central objects (resources under templates/central) |
| central.podAnnotations | object | `{}` | Annotations to be added to ovn-central pods |
| central.podLabels | object | `{}` | Labels to be added to ovn-central pods |
| central.resources | object | `{"limits":{"cpu":"3","memory":"4Gi"},"requests":{"cpu":"300m","memory":"200Mi"}}` | ovn-central resource limits & requests ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| cni | object | `{"binaryDirectory":"/opt/cni/bin","configDirectory":"/etc/cni/net.d","configPriority":"01","localConfigFile":"/kube-ovn/01-kube-ovn.conflist","mountToolingDirectory":false,"toolingDirectory":"/usr/local/bin"}` | CNI binary/configuration injected on the nodes |
| cni.binaryDirectory | string | `"/opt/cni/bin"` | Location on the node where the agent will inject the Kube-OVN binary |
| cni.configDirectory | string | `"/etc/cni/net.d"` | Location of the CNI configuration on the node |
| cni.configPriority | string | `"01"` | Priority of Kube-OVN within the CNI configuration directory on the node Should be a string representing a double-digit integer |
| cni.localConfigFile | string | `"/kube-ovn/01-kube-ovn.conflist"` | Location of the CNI configuration inside the agent's pod |
| cni.mountToolingDirectory | bool | `false` | Whether to mount the node's tooling directory into the pod |
| cni.toolingDirectory | string | `"/usr/local/bin"` | Location on the node where the CNI will install Kube-OVN's tooling |
| controller | object | `{"annotations":{},"labels":{},"metrics":{"port":10660},"podAnnotations":{},"podLabels":{},"resources":{"limits":{"cpu":"1000m","memory":"1Gi"},"requests":{"cpu":"200m","memory":"200Mi"}}}` | Configuration for kube-ovn-controller, the controller responsible for syncing K8s with OVN |
| controller.annotations | object | `{}` | Annotations to be added to all top-level kube-ovn-controller objects (resources under templates/controller) |
| controller.labels | object | `{}` | Labels to be added to all top-level kube-ovn-controller objects (resources under templates/controller) |
| controller.metrics | object | `{"port":10660}` | Controller metrics configuration |
| controller.metrics.port | int | `10660` | Configure the port on which the controller service will serve metrics |
| controller.podAnnotations | object | `{}` | Annotations to be added to kube-ovn-controller pods |
| controller.podLabels | object | `{}` | Labels to be added to kube-ovn-controller pods |
| controller.resources | object | `{"limits":{"cpu":"1000m","memory":"1Gi"},"requests":{"cpu":"200m","memory":"200Mi"}}` | kube-ovn-controller resource limits & requests ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| extraObjects | list | `[]` | Array of extra K8s manifests to deploy # Note: Supports use of custom Helm templates (Go templating) |
| features | object | `{"CHECK_GATEWAY":true,"ENABLE_ANP":false,"ENABLE_BIND_LOCAL_IP":true,"ENABLE_EXTERNAL_VPC":true,"ENABLE_IC":false,"ENABLE_KEEP_VM_IP":true,"ENABLE_LB":true,"ENABLE_LB_SVC":false,"ENABLE_LIVE_MIGRATION_OPTIMIZE":true,"ENABLE_NAT_GW":true,"ENABLE_NP":true,"ENABLE_OVN_IPSEC":false,"ENABLE_OVN_LB_PREFER_LOCAL":false,"ENABLE_TPROXY":false,"HW_OFFLOAD":false,"LOGICAL_GATEWAY":false,"LS_CT_SKIP_DST_LPORT_IPS":true,"LS_DNAT_MOD_DL_DST":true,"OVSDB_CON_TIMEOUT":3,"OVSDB_INACTIVITY_TIMEOUT":10,"SECURE_SERVING":false,"SET_VXLAN_TX_OFF":false,"U2O_INTERCONNECTION":false}` | Features of Kube-OVN we wish to enable/disable |
| fullnameOverride | string | `""` |  |
| global.images.kubeovn.dpdkRepository | string | `"kube-ovn-dpdk"` |  |
| global.images.kubeovn.repository | string | `"kube-ovn"` |  |
| global.images.kubeovn.support_arm | bool | `true` |  |
| global.images.kubeovn.tag | string | `"v1.14.0"` |  |
| global.images.kubeovn.thirdparty | bool | `true` |  |
| global.images.kubeovn.vpcRepository | string | `"vpc-nat-gateway"` |  |
| global.registry.address | string | `"docker.io/kubeovn"` |  |
| global.registry.imagePullSecrets | list | `[]` |  |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| kubelet | object | `{"directory":"/var/lib/kubelet"}` | Kubelet configuration |
| kubelet.directory | string | `"/var/lib/kubelet"` | Directory in which the kubelet operates |
| logging | object | `{"directory":"/var/log"}` | Logging configuration for all the daemons |
| logging.directory | string | `"/var/log"` | Directory in which to write the logs |
| masterNodes | string | `""` | Comma-separated list of IPs for each master node |
| masterNodesLabel | string | `"kube-ovn/role=master"` | Label used to auto-identify masters |
| monitor | object | `{"annotations":{},"labels":{},"metrics":{"port":10661},"podAnnotations":{},"podLabels":{},"resources":{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}}` | Configuration for kube-ovn-monitor, the agent monitoring and returning metrics for the northbound/southbound DBs and northd |
| monitor.annotations | object | `{}` | Annotations to be added to all top-level kube-ovn-monitor objects (resources under templates/monitor) |
| monitor.labels | object | `{}` | Labels to be added to all top-level kube-ovn-monitor objects (resources under templates/monitor) |
| monitor.metrics | object | `{"port":10661}` | kube-ovn-monitor metrics configuration |
| monitor.metrics.port | int | `10661` | Configure the port on which the kube-ovn-monitor service will serve metrics |
| monitor.podAnnotations | object | `{}` | Annotations to be added to kube-ovn-monitor pods |
| monitor.podLabels | object | `{}` | Labels to be added to kube-ovn-monitor pods |
| monitor.resources | object | `{"limits":{"cpu":"200m","memory":"200Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}` | kube-ovn-monitor resource limits & requests ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| nameOverride | string | `""` |  |
| namespace | string | `"kube-system"` | Namespace in which the CNI is deployed |
| natGw | object | `{"bgpSpeaker":{"apiNadProvider":"{{ .Values.apiNad.name }}.{{ .Values.namespace }}.ovn","image":{"pullPolicy":"IfNotPresent","repository":"docker.io/kubeovn/kube-ovn","tag":"v1.14.0"}},"namePrefix":"vpc-nat-gw"}` | Configuration for the NAT gateways |
| natGw.bgpSpeaker | object | `{"apiNadProvider":"{{ .Values.apiNad.name }}.{{ .Values.namespace }}.ovn","image":{"pullPolicy":"IfNotPresent","repository":"docker.io/kubeovn/kube-ovn","tag":"v1.14.0"}}` | Configuration of the BGP sidecar for when a NAT gateway is running in BGP mode |
| natGw.bgpSpeaker.apiNadProvider | string | `"{{ .Values.apiNad.name }}.{{ .Values.namespace }}.ovn"` | Network attachment definition used to reach the API server when running on BGP mode By default, equals the value set at ".apiNad.provider", you will need to set ".apiNad.enabled" to true See https://kubeovn.github.io/docs/stable/en/advance/with-bgp/ |
| natGw.bgpSpeaker.image | object | `{"pullPolicy":"IfNotPresent","repository":"docker.io/kubeovn/kube-ovn","tag":"v1.14.0"}` | Image used by the NAT gateway sidecar |
| natGw.namePrefix | string | `"vpc-nat-gw"` | Prefix appended to the name of the NAT gateways when generating the Pods If this value is changed after NAT GWs have been provisioned, every NAT gateway will need to be manually destroyed and recreated |
| networking | object | `{"defaultVpcName":"ovn-cluster","enableCompact":false,"enableEcmp":false,"enableEipSnat":true,"enableMetrics":true,"enableSsl":false,"exchangeLinkName":false,"excludeIps":"","join":{"cidr":{"v4":"100.64.0.0/16","v6":"fd00:100:64::/112"},"subnetName":"join"},"networkType":"geneve","nodeLocalDnsIp":"","podNicType":"veth-pair","pods":{"cidr":{"v4":"10.16.0.0/16","v6":"fd00:10:16::/112"},"gateways":{"v4":"10.16.0.1","v6":"fd00:10:16::1"},"subnetName":"ovn-default"},"services":{"cidr":{"v4":"10.96.0.0/12","v6":"fd00:10:96::/112"}},"stack":"IPv4","tunnelType":"geneve","vlan":{"id":"100","interfaceName":"","name":"ovn-vlan","providerName":"provider"}}` | General configuration of the network created by Kube-OVN |
| networking.defaultVpcName | string | `"ovn-cluster"` | Name of the default VPC once it is generated in the cluster Pods in the default subnet live in this VPC |
| networking.enableEipSnat | bool | `true` | Enable EIP and SNAT |
| networking.enableMetrics | bool | `true` | Enable listening on the metrics endpoint for the CNI daemons |
| networking.enableSsl | bool | `false` | Deploy the CNI with SSL encryption in between components |
| networking.excludeIps | string | `""` | IPs to exclude from IPAM in the default subnet |
| networking.join | object | `{"cidr":{"v4":"100.64.0.0/16","v6":"fd00:100:64::/112"},"subnetName":"join"}` | Configuration of the "join" subnet, used by the nodes to contact (join) the pods in the default subnet If .networking.stack is set to IPv4, only the .v4 key is used If .networking.stack is set to IPv6, only the .v6 key is used If .networking.stack is set to Dual, both keys are used |
| networking.join.subnetName | string | `"join"` | Name of the join subnet once it gets generated in the cluster |
| networking.networkType | string | `"geneve"` | Network type can be geneve or vlan |
| networking.nodeLocalDnsIp | string | `""` | Comma-separated string of NodeLocal DNS IP addresses |
| networking.podNicType | string | `"veth-pair"` | NIC type used on pods to connect them to the CNI |
| networking.pods | object | `{"cidr":{"v4":"10.16.0.0/16","v6":"fd00:10:16::/112"},"gateways":{"v4":"10.16.0.1","v6":"fd00:10:16::1"},"subnetName":"ovn-default"}` | Configuration for the default pod subnet If .networking.stack is set to IPv4, only the .v4 key is used If .networking.stack is set to IPv6, only the .v6 key is used If .networking.stack is set to Dual, both keys are used |
| networking.pods.subnetName | string | `"ovn-default"` | Name of the pod subnet once it gets generated in the cluster |
| networking.services | object | `{"cidr":{"v4":"10.96.0.0/12","v6":"fd00:10:96::/112"}}` | Configuration for the service subnet If .networking.stack is set to IPv4, only the .v4 key is used If .networking.stack is set to IPv6, only the .v6 key is used If .networking.stack is set to Dual, both keys are used |
| networking.stack | string | `"IPv4"` | Protocol(s) used by Kube-OVN to allocate IPs to pods and services Can be either IPv4, IPv6 or Dual |
| networking.tunnelType | string | `"geneve"` | Tunnel type can be geneve, vxlan or stt |
| networking.vlan | object | `{"id":"100","interfaceName":"","name":"ovn-vlan","providerName":"provider"}` | Configuration if we're running on top of a VLAN |
| ovsOvn | object | `{"annotations":{},"disableModulesManagement":false,"dpdk":{"enabled":false,"resources":{"limits":{"cpu":"1000m","hugepages-1Gi":"1Gi","memory":"1000Mi"},"requests":{"cpu":"1000m","memory":"200Mi"}},"version":"19.11"},"dpdkHybrid":{"enabled":false,"resources":{"limits":{"cpu":"2","hugepages-2Mi":"1Gi","memory":"1000Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}},"labels":{},"ovnDirectory":"/etc/origin/ovn","ovnRemoteOpenflowInterval":180,"ovnRemoteProbeInterval":10000,"ovsDirectory":"/etc/origin/openvswitch","podAnnotations":{},"podLabels":{},"probeInterval":180000,"resources":{"limits":{"cpu":"2","memory":"1000Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}}` | Configuration for ovs-ovn, the Open vSwitch/Open Virtual Network daemons |
| ovsOvn.annotations | object | `{}` | Annotations to be added to all top-level ovs-ovn objects (resources under templates/ovs-ovn) |
| ovsOvn.disableModulesManagement | bool | `false` | Disable auto-loading of kernel modules by OVS If this is disabled, you will have to enable the Open vSwitch kernel module yourself |
| ovsOvn.dpdk | object | `{"enabled":false,"resources":{"limits":{"cpu":"1000m","hugepages-1Gi":"1Gi","memory":"1000Mi"},"requests":{"cpu":"1000m","memory":"200Mi"}},"version":"19.11"}` | DPDK support for OVS ref: https://kubeovn.github.io/docs/v1.12.x/en/advance/dpdk/ |
| ovsOvn.dpdk.enabled | bool | `false` | Enables DPDK support on OVS |
| ovsOvn.dpdk.resources | object | `{"limits":{"cpu":"1000m","hugepages-1Gi":"1Gi","memory":"1000Mi"},"requests":{"cpu":"1000m","memory":"200Mi"}}` | ovs-ovn resource limits & requests when DPDK is enabled ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| ovsOvn.dpdk.version | string | `"19.11"` | Version of the DPDK image |
| ovsOvn.dpdkHybrid | object | `{"enabled":false,"resources":{"limits":{"cpu":"2","hugepages-2Mi":"1Gi","memory":"1000Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}}` | DPDK-hybrid support for OVS ref: https://kubeovn.github.io/docs/v1.12.x/en/advance/dpdk/ |
| ovsOvn.dpdkHybrid.enabled | bool | `false` | Enables DPDK-hybrid support on OVS |
| ovsOvn.dpdkHybrid.resources | object | `{"limits":{"cpu":"2","hugepages-2Mi":"1Gi","memory":"1000Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}` | ovs-ovn resource limits & requests when DPDK-hybrid is enabled ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| ovsOvn.labels | object | `{}` | Labels to be added to all top-level ovs-ovn objects (resources under templates/ovs-ovn) |
| ovsOvn.ovnDirectory | string | `"/etc/origin/ovn"` | Directory on the node where Open Virtual Network (OVN) lives |
| ovsOvn.ovsDirectory | string | `"/etc/origin/openvswitch"` | Directory on the node where Open vSwitch (OVS) lives |
| ovsOvn.podAnnotations | object | `{}` | Annotations to be added to ovs-ovn pods |
| ovsOvn.podLabels | object | `{}` | Labels to be added to ovs-ovn pods |
| ovsOvn.resources | object | `{"limits":{"cpu":"2","memory":"1000Mi"},"requests":{"cpu":"200m","memory":"200Mi"}}` | ovs-ovn resource limits & requests, overridden if DPDK is enabled ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| performance | object | `{"gcInterval":360,"inspectInterval":20,"ovsVsctlConcurrency":100}` | Performance tuning parameters |
| pinger | object | `{"annotations":{},"labels":{},"metrics":{"port":8080},"podAnnotations":{},"podLabels":{},"resources":{"limits":{"cpu":"200m","memory":"400Mi"},"requests":{"cpu":"100m","memory":"100Mi"}},"targets":{"externalAddresses":{"v4":"1.1.1.1","v6":"2606:4700:4700::1111"},"externalDomain":{"v4":"kube-ovn.io.","v6":"google.com."}}}` | Configuration for kube-ovn-pinger, the agent monitoring and returning metrics for OVS/external connectivity |
| pinger.annotations | object | `{}` | Annotations to be added to all top-level kube-ovn-pinger objects (resources under templates/pinger) |
| pinger.labels | object | `{}` | Labels to be added to all top-level kube-ovn-pinger objects (resources under templates/pinger) |
| pinger.metrics | object | `{"port":8080}` | kube-ovn-pinger metrics configuration |
| pinger.metrics.port | int | `8080` | Configure the port on which the kube-ovn-monitor service will serve metrics |
| pinger.podAnnotations | object | `{}` | Annotations to be added to kube-ovn-pinger pods |
| pinger.podLabels | object | `{}` | Labels to be added to kube-ovn-pinger pods |
| pinger.resources | object | `{"limits":{"cpu":"200m","memory":"400Mi"},"requests":{"cpu":"100m","memory":"100Mi"}}` | kube-ovn-pinger resource limits & requests ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| pinger.targets | object | `{"externalAddresses":{"v4":"1.1.1.1","v6":"2606:4700:4700::1111"},"externalDomain":{"v4":"kube-ovn.io.","v6":"google.com."}}` | Remote targets used by the pinger daemon to determine if the CNI works and has external connectivity |
| pinger.targets.externalAddresses | object | `{"v4":"1.1.1.1","v6":"2606:4700:4700::1111"}` | Raw IPv4/6 on which to issue pings |
| pinger.targets.externalDomain | object | `{"v4":"kube-ovn.io.","v6":"google.com."}` | Domains to resolve and to ping Make sure the v6 domain resolves both A and AAAA records, while the v4 only resolves A records |
| speaker | object | `{"annotations":{},"args":[],"enabled":false,"labels":{},"nodeSelector":{},"podAnnotations":{},"podLabels":{},"resources":{"limits":{},"requests":{"cpu":"500m","memory":"300Mi"}}}` | Configuration for kube-ovn-speaker, the BGP speaker announcing routes to the external world |
| speaker.annotations | object | `{}` | Annotations to be added to all top-level kube-ovn-speaker objects (resources under templates/speaker) |
| speaker.enabled | bool | `false` | Enable the kube-ovn-speaker |
| speaker.labels | object | `{}` | Labels to be added to all top-level kube-ovn-speaker objects (resources under templates/speaker) |
| speaker.nodeSelector | object | `{}` | Node selector to restrict the deployment of the speaker to specific nodes |
| speaker.podAnnotations | object | `{}` | Annotations to be added to kube-ovn-speaker pods |
| speaker.podLabels | object | `{}` | Labels to be added to kube-ovn-speaker pods |
| speaker.resources | object | `{"limits":{},"requests":{"cpu":"500m","memory":"300Mi"}}` | kube-ovn-speaker resource limits & requests ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/ |
| validatingWebhook | object | `{"annotations":{},"enabled":false,"labels":{},"podAnnotations":{},"podLabels":{}}` | Configuration of the validating webhook used to verify custom resources before they are pushed to Kubernetes. Make sure cert-manager is installed for the generation of certificates for the webhook See https://kubeovn.github.io/docs/stable/en/guide/webhook/ |
| validatingWebhook.annotations | object | `{}` | Annotations to be added to all top-level kube-ovn-webhook objects (resources under templates/webhook) |
| validatingWebhook.enabled | bool | `false` | Enable the deployment of the validating webhook |
| validatingWebhook.labels | object | `{}` | Labels to be added to all top-level kube-ovn-webhook objects (resources under templates/webhook) |
| validatingWebhook.podAnnotations | object | `{}` | Annotations to be added to kube-ovn-webhook pods |
| validatingWebhook.podLabels | object | `{}` | Labels to be added to kube-ovn-webhook pods |

