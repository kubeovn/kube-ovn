# Helm chart for Kube-OVN

![Version: 1.15.0](https://img.shields.io/badge/Version-1.15.0-informational?style=flat-square)  ![Version: 1.15.0](https://img.shields.io/badge/Version-1.15.0-informational?style=flat-square)

This is the v2 of the Helm Chart, replacing the first version in the long term.
Make sure to adjust your old values with the new ones and pre-generate your templates with a dry-run to ensure no breaking change occurs.

## Installing the Chart

### From OCI Registry

The Helm chart is available from GitHub Container Registry:

```bash
helm install kube-ovn oci://ghcr.io/kubeovn/charts/kube-ovn-v2 --version 1.15.0
```

### From Source

```bash
helm install kube-ovn ./charts/kube-ovn-v2
```

## How to install Kube-OVN on Talos Linux

To install Kube-OVN on Talos Linux, declare the **OpenvSwitch** module in the `machine` config of your Talos install:

```yaml
machine:
  kernel:
    modules:
    - name: openvswitch
```

Then use the following options to install this chart:

```yaml
ovsOvn:
  disableModulesManagement: true
  ovsDirectory: "/var/lib/openvswitch"
  ovnDirectory: "/var/lib/ovn"
cni:
  mountToolingDirectory: false
```

## How to regenerate this README

This README is generated using [helm-docs](https://github.com/norwoodj/helm-docs). Launch `helm-docs` while in this folder to regenerate the documented values.

## Values

<h3>CNI agent configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>agent</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for kube-ovn-cni, the agent responsible for handling CNI requests from the CRI.</td>
		</tr>
		<tr>
			<td>agent.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level agent objects (resources under templates/agent)</td>
		</tr>
		<tr>
			<td>agent.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level agent objects (resources under templates/agent)</td>
		</tr>
		<tr>
			<td>agent.metrics</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Agent metrics configuration.</td>
		</tr>
		<tr>
			<td>agent.metrics.port</td>
			<td>int</td>
			<td><pre lang="json">
10665
</pre>
</td>
			<td>Configure the port on which the agent service will serve metrics.</td>
		</tr>
		<tr>
			<td>agent.mirroring</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Mirroring of the traffic for debug or analysis. https://kubeovn.github.io/docs/stable/en/guide/mirror/</td>
		</tr>
		<tr>
			<td>agent.mirroring.enabled</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable mirroring of the traffic.</td>
		</tr>
		<tr>
			<td>agent.mirroring.interface</td>
			<td>string</td>
			<td><pre lang="json">
"mirror0"
</pre>
</td>
			<td>Interface on which to send the mirrored traffic.</td>
		</tr>
		<tr>
			<td>agent.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to the agent pods (kube-ovn-cni)</td>
		</tr>
		<tr>
			<td>agent.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to the agent pods (kube-ovn-cni)</td>
		</tr>
		<tr>
			<td>agent.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {
    "cpu": "1000m",
    "memory": "1Gi"
  },
  "requests": {
    "cpu": "100m",
    "memory": "100Mi"
  }
}
</pre>
</td>
			<td>Agent daemon resource limits & requests. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
	</tbody>
</table>
<h3>CNI agent configuration.</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>agent.dpdkTunnelInterface</td>
			<td>string</td>
			<td><pre lang="json">
"br-phy"
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>agent.interface</td>
			<td>string</td>
			<td><pre lang="json">
""
</pre>
</td>
			<td>""</td>
		</tr>
	</tbody>
</table>
<h3>API Network Attachment Definition configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>apiNad</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>API NetworkAttachmentDefinition to give some pods (CoreDNS, NAT GW) in custom VPCs access to the K8S API. This requires Multus to be installed.</td>
		</tr>
		<tr>
			<td>apiNad.enabled</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable the creation of the API NAD.</td>
		</tr>
		<tr>
			<td>apiNad.name</td>
			<td>string</td>
			<td><pre lang="json">
"ovn-kubernetes-api"
</pre>
</td>
			<td>Name of the NAD.</td>
		</tr>
		<tr>
			<td>apiNad.provider</td>
			<td>string</td>
			<td><pre lang="json">
"{{ .Values.apiNad.name }}.{{ .Values.namespace }}.ovn"
</pre>
</td>
			<td>Name of the provider, must be in the form "nadName.nadNamespace.ovn".</td>
		</tr>
		<tr>
			<td>apiNad.subnet</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Subnet associated with the NAD, it will have full access to the API server.</td>
		</tr>
		<tr>
			<td>apiNad.subnet.cidrBlock</td>
			<td>string</td>
			<td><pre lang="json">
"100.100.0.0/16,fd00:100:100::/112"
</pre>
</td>
			<td>CIDR block used by the API subnet.</td>
		</tr>
		<tr>
			<td>apiNad.subnet.name</td>
			<td>string</td>
			<td><pre lang="json">
"ovn-kubernetes-api"
</pre>
</td>
			<td>Name of the subnet.</td>
		</tr>
		<tr>
			<td>apiNad.subnet.protocol</td>
			<td>string</td>
			<td><pre lang="json">
"Dual"
</pre>
</td>
			<td>Protocol for the API subnet.</td>
		</tr>
	</tbody>
</table>
<h3>BGP speaker configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>bgpSpeaker</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for kube-ovn-speaker, the BGP speaker announcing routes to the external world.</td>
		</tr>
		<tr>
			<td>bgpSpeaker.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level kube-ovn-speaker objects (resources under templates/speaker)</td>
		</tr>
		<tr>
			<td>bgpSpeaker.args</td>
			<td>list</td>
			<td><pre lang="json">
[]
</pre>
</td>
			<td>Args passed to the kube-ovn-speaker pod.</td>
		</tr>
		<tr>
			<td>bgpSpeaker.enabled</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable the kube-ovn-speaker.</td>
		</tr>
		<tr>
			<td>bgpSpeaker.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level kube-ovn-speaker objects (resources under templates/speaker)</td>
		</tr>
		<tr>
			<td>bgpSpeaker.nodeSelector</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Node selector to restrict the deployment of the speaker to specific nodes.</td>
		</tr>
		<tr>
			<td>bgpSpeaker.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to kube-ovn-speaker pods.</td>
		</tr>
		<tr>
			<td>bgpSpeaker.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to kube-ovn-speaker pods.</td>
		</tr>
		<tr>
			<td>bgpSpeaker.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {},
  "requests": {
    "cpu": "500m",
    "memory": "300Mi"
  }
}
</pre>
</td>
			<td>kube-ovn-speaker resource limits & requests. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
	</tbody>
</table>
<h3>OVN-central daemon configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>central</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for ovn-central, the daemon containing the northbound/southbound DBs and northd.</td>
		</tr>
		<tr>
			<td>central.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level ovn-central objects (resources under templates/central)</td>
		</tr>
		<tr>
			<td>central.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level ovn-central objects (resources under templates/central)</td>
		</tr>
		<tr>
			<td>central.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to ovn-central pods.</td>
		</tr>
		<tr>
			<td>central.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to ovn-central pods.</td>
		</tr>
		<tr>
			<td>central.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {
    "cpu": "3",
    "memory": "4Gi"
  },
  "requests": {
    "cpu": "300m",
    "memory": "200Mi"
  }
}
</pre>
</td>
			<td>ovn-central resource limits & requests. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
	</tbody>
</table>
<h3>OVN-central daemon configuration.</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>central.ovnLeaderProbeInterval</td>
			<td>int</td>
			<td><pre lang="json">
5
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>central.ovnNorthdNThreads</td>
			<td>int</td>
			<td><pre lang="json">
1
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>central.ovnNorthdProbeInterval</td>
			<td>int</td>
			<td><pre lang="json">
5000
</pre>
</td>
			<td>""</td>
		</tr>
	</tbody>
</table>
<h3>Global parameters</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>clusterDomain</td>
			<td>string</td>
			<td><pre lang="json">
"cluster.local"
</pre>
</td>
			<td>Domain used by the cluster.</td>
		</tr>
		<tr>
			<td>fullnameOverride</td>
			<td>string</td>
			<td><pre lang="json">
""
</pre>
</td>
			<td>Full name override.</td>
		</tr>
		<tr>
			<td>global</td>
			<td>object</td>
			<td><pre lang="json">
{
  "images": {
    "kubeovn": {
      "repository": "kube-ovn",
      "tag": "v1.14.0"
    }
  },
  "registry": {
    "address": "docker.io/kubeovn",
    "imagePullSecrets": []
  }
}
</pre>
</td>
			<td>Global configuration.</td>
		</tr>
		<tr>
			<td>image</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Image configuration.</td>
		</tr>
		<tr>
			<td>image.pullPolicy</td>
			<td>string</td>
			<td><pre lang="json">
"IfNotPresent"
</pre>
</td>
			<td>Pull policy for all images.</td>
		</tr>
		<tr>
			<td>masterNodes</td>
			<td>list</td>
			<td><pre lang="json">
[]
</pre>
</td>
			<td>Comma-separated list of IPs for each master node. If not specified, fallback to auto-identifying masters based on "masterNodesLabels"</td>
		</tr>
		<tr>
			<td>masterNodesLabels</td>
			<td>object</td>
			<td><pre lang="json">
{
  "kube-ovn/role": "master"
}
</pre>
</td>
			<td>Label used to auto-identify masters. Any node that has any of these labels will be considered a master node. Note: This feature uses Helm "lookup" function, which is not compatible with tools such as ArgoCD.</td>
		</tr>
		<tr>
			<td>nameOverride</td>
			<td>string</td>
			<td><pre lang="json">
""
</pre>
</td>
			<td>Name override.</td>
		</tr>
		<tr>
			<td>namespace</td>
			<td>string</td>
			<td><pre lang="json">
"kube-system"
</pre>
</td>
			<td>Namespace in which the CNI is deployed.</td>
		</tr>
	</tbody>
</table>
<h3>CNI configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>cni</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>CNI binary/configuration injected on the nodes.</td>
		</tr>
		<tr>
			<td>cni.binaryDirectory</td>
			<td>string</td>
			<td><pre lang="json">
"/opt/cni/bin"
</pre>
</td>
			<td>Location on the node where the agent will inject the Kube-OVN binary.</td>
		</tr>
		<tr>
			<td>cni.configDirectory</td>
			<td>string</td>
			<td><pre lang="json">
"/etc/cni/net.d"
</pre>
</td>
			<td>Location of the CNI configuration on the node.</td>
		</tr>
		<tr>
			<td>cni.configPriority</td>
			<td>string</td>
			<td><pre lang="json">
"01"
</pre>
</td>
			<td>Priority of Kube-OVN within the CNI configuration directory on the node. Should be a string representing a double-digit integer.</td>
		</tr>
		<tr>
			<td>cni.localConfigFile</td>
			<td>string</td>
			<td><pre lang="json">
"/kube-ovn/01-kube-ovn.conflist"
</pre>
</td>
			<td>Location of the CNI configuration inside the agent's pod.</td>
		</tr>
		<tr>
			<td>cni.mountConfigDirectory</td>
			<td>string</td>
			<td><pre lang="json">
"/etc/cni/net.d"
</pre>
</td>
			<td>Location of the CNI configuration to be mounted inside the pod.</td>
		</tr>
		<tr>
			<td>cni.mountToolingDirectory</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Whether to mount the node's tooling directory into the pod.</td>
		</tr>
		<tr>
			<td>cni.nonPrimaryCNI</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Whether to use Kube-OVN as non-primary CNI. When set to true, Kube-OVN will not allocate/handle primary network interfaces. Interfaces are created using Network Attachment Definitions (NADs)</td>
		</tr>
		<tr>
			<td>cni.toolingDirectory</td>
			<td>string</td>
			<td><pre lang="json">
"/usr/local/bin"
</pre>
</td>
			<td>Location on the node where the CNI will install Kube-OVN's tooling.</td>
		</tr>
	</tbody>
</table>
<h3>Kube-OVN controller configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>controller</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for kube-ovn-controller, the controller responsible for syncing K8s with OVN.</td>
		</tr>
		<tr>
			<td>controller.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level kube-ovn-controller objects (resources under templates/controller)</td>
		</tr>
		<tr>
			<td>controller.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level kube-ovn-controller objects (resources under templates/controller)</td>
		</tr>
		<tr>
			<td>controller.metrics</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Controller metrics configuration.</td>
		</tr>
		<tr>
			<td>controller.metrics.port</td>
			<td>int</td>
			<td><pre lang="json">
10660
</pre>
</td>
			<td>Configure the port on which the controller service will serve metrics.</td>
		</tr>
		<tr>
			<td>controller.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to kube-ovn-controller pods.</td>
		</tr>
		<tr>
			<td>controller.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to kube-ovn-controller pods.</td>
		</tr>
		<tr>
			<td>controller.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {
    "cpu": "1000m",
    "memory": "1Gi"
  },
  "requests": {
    "cpu": "200m",
    "memory": "200Mi"
  }
}
</pre>
</td>
			<td>kube-ovn-controller resource limits & requests. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
	</tbody>
</table>
<h3>Extra objects</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>extraObjects</td>
			<td>list</td>
			<td><pre lang="json">
[]
</pre>
</td>
			<td>Array of extra K8s manifests to deploy. Note: Supports use of custom Helm templates (Go templating)</td>
		</tr>
	</tbody>
</table>
<h3>Opt-in/out Features</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>features</td>
			<td>object</td>
			<td><pre lang="json">
{
  "ENABLE_ANP": false,
  "ENABLE_BIND_LOCAL_IP": true,
  "ENABLE_DNS_NAME_RESOLVER": false,
  "ENABLE_OVN_LB_PREFER_LOCAL": false,
  "LS_CT_SKIP_DST_LPORT_IPS": true,
  "LS_DNAT_MOD_DL_DST": true,
  "OVSDB_CON_TIMEOUT": 3,
  "OVSDB_INACTIVITY_TIMEOUT": 10,
  "SET_VXLAN_TX_OFF": false,
  "enableExternalVpcs": false,
  "enableHardwareOffload": false,
  "enableKeepVmIps": true,
  "enableLiveMigrationOptimization": true,
  "enableLoadbalancer": true,
  "enableLoadbalancerService": false,
  "enableNatGateways": true,
  "enableNetworkPolicies": true,
  "enableOvnInterconnections": false,
  "enableOvnIpsec": false,
  "enableSecureServing": false,
  "enableTproxy": false,
  "enableU2OInterconnections": false
}
</pre>
</td>
			<td>Features of Kube-OVN we wish to enable/disable.</td>
		</tr>
		<tr>
			<td>features.enableExternalVpcs</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable external VPCs</td>
		</tr>
		<tr>
			<td>features.enableHardwareOffload</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable hardware offloads</td>
		</tr>
		<tr>
			<td>features.enableKeepVmIps</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable persistent VM IPs</td>
		</tr>
		<tr>
			<td>features.enableLiveMigrationOptimization</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable optimized live migrations for VMs</td>
		</tr>
		<tr>
			<td>features.enableLoadbalancer</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable Kube-OVN loadbalancers</td>
		</tr>
		<tr>
			<td>features.enableLoadbalancerService</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable Kube-OVN loadbalancer services</td>
		</tr>
		<tr>
			<td>features.enableNatGateways</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable NAT gateways</td>
		</tr>
		<tr>
			<td>features.enableNetworkPolicies</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable Kube-OVN network policies</td>
		</tr>
		<tr>
			<td>features.enableOvnInterconnections</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable OVN interconnections</td>
		</tr>
		<tr>
			<td>features.enableOvnIpsec</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable IPSEC</td>
		</tr>
		<tr>
			<td>features.enableSecureServing</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable secure serving</td>
		</tr>
		<tr>
			<td>features.enableTproxy</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable TProxy</td>
		</tr>
		<tr>
			<td>features.enableU2OInterconnections</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable underlay to overlay interconnections</td>
		</tr>
	</tbody>
</table>
<h3>Kubelet configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>kubelet</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Kubelet configuration.</td>
		</tr>
		<tr>
			<td>kubelet.directory</td>
			<td>string</td>
			<td><pre lang="json">
"/var/lib/kubelet"
</pre>
</td>
			<td>Directory in which the kubelet operates.</td>
		</tr>
		<tr>
			<td>logging.directory</td>
			<td>string</td>
			<td><pre lang="json">
"/var/log"
</pre>
</td>
			<td>Directory in which to write the logs.</td>
		</tr>
	</tbody>
</table>
<h3>Logging configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>logging</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Logging configuration for all the daemons.</td>
		</tr>
	</tbody>
</table>
<h3>OVN monitoring daemon configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>monitor</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for kube-ovn-monitor, the agent monitoring and returning metrics for the northbound/southbound DBs and northd.</td>
		</tr>
		<tr>
			<td>monitor.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level kube-ovn-monitor objects (resources under templates/monitor)</td>
		</tr>
		<tr>
			<td>monitor.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level kube-ovn-monitor objects (resources under templates/monitor)</td>
		</tr>
		<tr>
			<td>monitor.metrics</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>kube-ovn-monitor metrics configuration.</td>
		</tr>
		<tr>
			<td>monitor.metrics.port</td>
			<td>int</td>
			<td><pre lang="json">
10661
</pre>
</td>
			<td>Configure the port on which the kube-ovn-monitor service will serve metrics.</td>
		</tr>
		<tr>
			<td>monitor.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to kube-ovn-monitor pods.</td>
		</tr>
		<tr>
			<td>monitor.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to kube-ovn-monitor pods.</td>
		</tr>
		<tr>
			<td>monitor.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {
    "cpu": "200m",
    "memory": "200Mi"
  },
  "requests": {
    "cpu": "200m",
    "memory": "200Mi"
  }
}
</pre>
</td>
			<td>kube-ovn-monitor resource limits & requests. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
	</tbody>
</table>
<h3>NAT gateways configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>natGw</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for the NAT gateways.</td>
		</tr>
		<tr>
			<td>natGw.bgpSpeaker</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration of the BGP sidecar for when a NAT gateway is running in BGP mode.</td>
		</tr>
		<tr>
			<td>natGw.bgpSpeaker.apiNadProvider</td>
			<td>string</td>
			<td><pre lang="json">
"{{ .Values.apiNad.name }}.{{ .Values.namespace }}.ovn"
</pre>
</td>
			<td>Network attachment definition used to reach the API server when running on BGP mode. By default, equals the value set at ".apiNad.provider", you will need to set ".apiNad.enabled" to true. See https://kubeovn.github.io/docs/stable/en/advance/with-bgp/</td>
		</tr>
		<tr>
			<td>natGw.bgpSpeaker.image</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Image used by the NAT gateway sidecar.</td>
		</tr>
		<tr>
			<td>natGw.bgpSpeaker.image.pullPolicy</td>
			<td>string</td>
			<td><pre lang="json">
"IfNotPresent"
</pre>
</td>
			<td>Image pull policy.</td>
		</tr>
		<tr>
			<td>natGw.bgpSpeaker.image.repository</td>
			<td>string</td>
			<td><pre lang="json">
"docker.io/kubeovn/kube-ovn"
</pre>
</td>
			<td>Image repository.</td>
		</tr>
		<tr>
			<td>natGw.bgpSpeaker.image.tag</td>
			<td>string</td>
			<td><pre lang="json">
"v1.15.0"
</pre>
</td>
			<td>Image tag.</td>
		</tr>
		<tr>
			<td>natGw.image</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Image used by the NAT gateway.</td>
		</tr>
		<tr>
			<td>natGw.image.pullPolicy</td>
			<td>string</td>
			<td><pre lang="json">
"IfNotPresent"
</pre>
</td>
			<td>Image pull policy.</td>
		</tr>
		<tr>
			<td>natGw.image.repository</td>
			<td>string</td>
			<td><pre lang="json">
"docker.io/kubeovn/vpc-nat-gateway"
</pre>
</td>
			<td>Image repository.</td>
		</tr>
		<tr>
			<td>natGw.image.tag</td>
			<td>string</td>
			<td><pre lang="json">
"v1.15.0"
</pre>
</td>
			<td>Image tag.</td>
		</tr>
		<tr>
			<td>natGw.namePrefix</td>
			<td>string</td>
			<td><pre lang="json">
"vpc-nat-gw"
</pre>
</td>
			<td>Prefix appended to the name of the NAT gateways when generating the Pods. If this value is changed after NAT GWs have been provisioned, every NAT gateway will need to be manually destroyed and recreated.</td>
		</tr>
	</tbody>
</table>
<h3>Network Policies</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>networkPolicies</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for network policies</td>
		</tr>
		<tr>
			<td>networkPolicies.enforcement</td>
			<td>string</td>
			<td><pre lang="json">
"standard"
</pre>
</td>
			<td>Enforcement level of network policies when they get applied (can be: standard, lax). Enforcement "standard" blocks everything except what is allowed by the network policies. Enforcement "lax" is similar to "standard" with the exception that ARP/DHCPv4/DHCPv6/ICMPv4/ICMPv6 is allowed by default. This mode is useful when using Kubevirt and VMs with IPs configured via Kube-OVN's DHCP.</td>
		</tr>
	</tbody>
</table>
<h3>Network parameters of the CNI</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>networking</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>General configuration of the network created by Kube-OVN.</td>
		</tr>
		<tr>
			<td>networking.defaultVpcName</td>
			<td>string</td>
			<td><pre lang="json">
"ovn-cluster"
</pre>
</td>
			<td>Name of the default VPC once it is generated in the cluster. Pods in the default subnet live in this VPC.</td>
		</tr>
		<tr>
			<td>networking.enableCompact</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>networking.enableEcmp</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>networking.enableEipSnat</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable EIP and SNAT.</td>
		</tr>
		<tr>
			<td>networking.enableMetrics</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable listening on the metrics endpoint for the CNI daemons.</td>
		</tr>
		<tr>
			<td>networking.enableSsl</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Deploy the CNI with SSL encryption in between components.</td>
		</tr>
		<tr>
			<td>networking.exchangeLinkName</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>networking.excludeIps</td>
			<td>string</td>
			<td><pre lang="json">
""
</pre>
</td>
			<td>IPs to exclude from IPAM in the default subnet.</td>
		</tr>
		<tr>
			<td>networking.join</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration of the "join" subnet, used by the nodes to contact (join) the pods in the default subnet. If .networking.stack is set to IPv4, only the .v4 key is used. If .networking.stack is set to IPv6, only the .v6 key is used. If .networking.stack is set to Dual, both keys are used.</td>
		</tr>
		<tr>
			<td>networking.join.cidr</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>CIDR used by the join subnet.</td>
		</tr>
		<tr>
			<td>networking.join.cidr.v4</td>
			<td>string</td>
			<td><pre lang="json">
"100.64.0.0/16"
</pre>
</td>
			<td>IPv4 CIDR.</td>
		</tr>
		<tr>
			<td>networking.join.cidr.v6</td>
			<td>string</td>
			<td><pre lang="json">
"fd00:100:64::/112"
</pre>
</td>
			<td>IPv6 CIDR.</td>
		</tr>
		<tr>
			<td>networking.join.subnetName</td>
			<td>string</td>
			<td><pre lang="json">
"join"
</pre>
</td>
			<td>Name of the join subnet once it gets generated in the cluster.</td>
		</tr>
		<tr>
			<td>networking.networkType</td>
			<td>string</td>
			<td><pre lang="json">
"geneve"
</pre>
</td>
			<td>Network type can be "geneve" or "vlan".</td>
		</tr>
		<tr>
			<td>networking.nodeLocalDnsIp</td>
			<td>string</td>
			<td><pre lang="json">
""
</pre>
</td>
			<td>Comma-separated string of NodeLocal DNS IP addresses.</td>
		</tr>
		<tr>
			<td>networking.podNicType</td>
			<td>string</td>
			<td><pre lang="json">
"veth-pair"
</pre>
</td>
			<td>NIC type used on pods to connect them to the CNI.</td>
		</tr>
		<tr>
			<td>networking.pods</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for the default pod subnet. If .networking.stack is set to IPv4, only the .v4 key is used. If .networking.stack is set to IPv6, only the .v6 key is used. If .networking.stack is set to Dual, both keys are used.</td>
		</tr>
		<tr>
			<td>networking.pods.cidr</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>CIDR used by the pods subnet.</td>
		</tr>
		<tr>
			<td>networking.pods.cidr.v4</td>
			<td>string</td>
			<td><pre lang="json">
"10.16.0.0/16"
</pre>
</td>
			<td>IPv4 CIDR.</td>
		</tr>
		<tr>
			<td>networking.pods.cidr.v6</td>
			<td>string</td>
			<td><pre lang="json">
"fd00:10:16::/112"
</pre>
</td>
			<td>IPv6 CIDR.</td>
		</tr>
		<tr>
			<td>networking.pods.enableGatewayChecks</td>
			<td>bool</td>
			<td><pre lang="json">
true
</pre>
</td>
			<td>Enable default gateway checks.</td>
		</tr>
		<tr>
			<td>networking.pods.enableLogicalGateways</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable logical gateways.</td>
		</tr>
		<tr>
			<td>networking.pods.gateways</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Gateways used in the pod subnet.</td>
		</tr>
		<tr>
			<td>networking.pods.gateways.v4</td>
			<td>string</td>
			<td><pre lang="json">
"10.16.0.1"
</pre>
</td>
			<td>IPv4 gateway.</td>
		</tr>
		<tr>
			<td>networking.pods.gateways.v6</td>
			<td>string</td>
			<td><pre lang="json">
"fd00:10:16::1"
</pre>
</td>
			<td>IPv6 gateway.</td>
		</tr>
		<tr>
			<td>networking.pods.mtu</td>
			<td>int</td>
			<td><pre lang="json">
0
</pre>
</td>
			<td>MTU of the subnet. If set to 0, the MTU is auto-detected.</td>
		</tr>
		<tr>
			<td>networking.pods.subnetName</td>
			<td>string</td>
			<td><pre lang="json">
"ovn-default"
</pre>
</td>
			<td>Name of the pod subnet once it gets generated in the cluster.</td>
		</tr>
		<tr>
			<td>networking.services</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for the service subnet. If .networking.stack is set to IPv4, only the .v4 key is used. If .networking.stack is set to IPv6, only the .v6 key is used. If .networking.stack is set to Dual, both keys are used.</td>
		</tr>
		<tr>
			<td>networking.services.cidr</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>CIDR used by the service subnet.</td>
		</tr>
		<tr>
			<td>networking.services.cidr.v4</td>
			<td>string</td>
			<td><pre lang="json">
"10.96.0.0/12"
</pre>
</td>
			<td>IPv4 CIDR.</td>
		</tr>
		<tr>
			<td>networking.services.cidr.v6</td>
			<td>string</td>
			<td><pre lang="json">
"fd00:10:96::/112"
</pre>
</td>
			<td>IPv6 CIDR.</td>
		</tr>
		<tr>
			<td>networking.skipConntrackDstCidrs</td>
			<td>string</td>
			<td><pre lang="json">
""
</pre>
</td>
			<td>Comma-separated list of destination IP CIDRs that should skip conntrack processing.</td>
		</tr>
		<tr>
			<td>networking.stack</td>
			<td>string</td>
			<td><pre lang="json">
"IPv4"
</pre>
</td>
			<td>Protocol(s) used by Kube-OVN to allocate IPs to pods and services. Can be either IPv4, IPv6 or Dual.</td>
		</tr>
		<tr>
			<td>networking.tunnelType</td>
			<td>string</td>
			<td><pre lang="json">
"geneve"
</pre>
</td>
			<td>Tunnel type can be "geneve", "vxlan" or "stt".</td>
		</tr>
		<tr>
			<td>networking.vlan</td>
			<td>object</td>
			<td><pre lang="json">
{
  "id": "100",
  "interfaceName": "",
  "name": "ovn-vlan",
  "providerName": "provider"
}
</pre>
</td>
			<td>Configuration if we're running on top of a VLAN.</td>
		</tr>
	</tbody>
</table>
<h3>OVS/OVN daemons configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>ovsOvn</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for ovs-ovn, the Open vSwitch/Open Virtual Network daemons.</td>
		</tr>
		<tr>
			<td>ovsOvn.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level ovs-ovn objects (resources under templates/ovs-ovn)</td>
		</tr>
		<tr>
			<td>ovsOvn.disableModulesManagement</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Disable auto-loading of kernel modules by OVS. If this is disabled, you will have to enable the Open vSwitch kernel module yourself.</td>
		</tr>
		<tr>
			<td>ovsOvn.dpdkHybrid</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>DPDK-hybrid support for OVS. ref: https://kubeovn.github.io/docs/v1.12.x/en/advance/dpdk/</td>
		</tr>
		<tr>
			<td>ovsOvn.dpdkHybrid.enabled</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enables DPDK-hybrid support on OVS.</td>
		</tr>
		<tr>
			<td>ovsOvn.dpdkHybrid.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {
    "cpu": "2",
    "hugepages-2Mi": "1Gi",
    "memory": "1000Mi"
  },
  "requests": {
    "cpu": "200m",
    "memory": "200Mi"
  }
}
</pre>
</td>
			<td>ovs-ovn resource limits & requests when DPDK-hybrid is enabled. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
		<tr>
			<td>ovsOvn.dpdkHybrid.tag</td>
			<td>string</td>
			<td><pre lang="json">
"v1.14.0-dpdk"
</pre>
</td>
			<td>DPDK image tag.</td>
		</tr>
		<tr>
			<td>ovsOvn.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level ovs-ovn objects (resources under templates/ovs-ovn)</td>
		</tr>
		<tr>
			<td>ovsOvn.ovnDirectory</td>
			<td>string</td>
			<td><pre lang="json">
"/etc/origin/ovn"
</pre>
</td>
			<td>Directory on the node where Open Virtual Network (OVN) lives.</td>
		</tr>
		<tr>
			<td>ovsOvn.ovsDirectory</td>
			<td>string</td>
			<td><pre lang="json">
"/etc/origin/openvswitch"
</pre>
</td>
			<td>Directory on the node where Open vSwitch (OVS) lives.</td>
		</tr>
		<tr>
			<td>ovsOvn.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to ovs-ovn pods.</td>
		</tr>
		<tr>
			<td>ovsOvn.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to ovs-ovn pods.</td>
		</tr>
		<tr>
			<td>ovsOvn.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {
    "cpu": "2",
    "memory": "1000Mi"
  },
  "requests": {
    "cpu": "200m",
    "memory": "200Mi"
  }
}
</pre>
</td>
			<td>ovs-ovn resource limits & requests. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
	</tbody>
</table>
<h3>Performance configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>performance</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Performance tuning parameters.</td>
		</tr>
		<tr>
			<td>performance.gcInterval</td>
			<td>int</td>
			<td><pre lang="json">
360
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>performance.inspectInterval</td>
			<td>int</td>
			<td><pre lang="json">
20
</pre>
</td>
			<td>""</td>
		</tr>
		<tr>
			<td>performance.ovsVsctlConcurrency</td>
			<td>int</td>
			<td><pre lang="json">
100
</pre>
</td>
			<td>""</td>
		</tr>
	</tbody>
</table>
<h3>Ping daemon configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>pinger</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration for kube-ovn-pinger, the agent monitoring and returning metrics for OVS/external connectivity.</td>
		</tr>
		<tr>
			<td>pinger.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level kube-ovn-pinger objects (resources under templates/pinger)</td>
		</tr>
		<tr>
			<td>pinger.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level kube-ovn-pinger objects (resources under templates/pinger)</td>
		</tr>
		<tr>
			<td>pinger.metrics</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>kube-ovn-pinger metrics configuration.</td>
		</tr>
		<tr>
			<td>pinger.metrics.port</td>
			<td>int</td>
			<td><pre lang="json">
8080
</pre>
</td>
			<td>Configure the port on which the kube-ovn-monitor service will serve metrics.</td>
		</tr>
		<tr>
			<td>pinger.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to kube-ovn-pinger pods.</td>
		</tr>
		<tr>
			<td>pinger.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to kube-ovn-pinger pods.</td>
		</tr>
		<tr>
			<td>pinger.resources</td>
			<td>object</td>
			<td><pre lang="json">
{
  "limits": {
    "cpu": "200m",
    "memory": "400Mi"
  },
  "requests": {
    "cpu": "100m",
    "memory": "100Mi"
  }
}
</pre>
</td>
			<td>kube-ovn-pinger resource limits & requests. ref: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/</td>
		</tr>
		<tr>
			<td>pinger.targets</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Remote targets used by the pinger daemon to determine if the CNI works and has external connectivity.</td>
		</tr>
		<tr>
			<td>pinger.targets.externalAddresses</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Raw IPv4/6 on which to issue pings.</td>
		</tr>
		<tr>
			<td>pinger.targets.externalAddresses.v4</td>
			<td>string</td>
			<td><pre lang="json">
"1.1.1.1"
</pre>
</td>
			<td>IPv4 address.</td>
		</tr>
		<tr>
			<td>pinger.targets.externalAddresses.v6</td>
			<td>string</td>
			<td><pre lang="json">
"2606:4700:4700::1111"
</pre>
</td>
			<td>IPv6 address.</td>
		</tr>
		<tr>
			<td>pinger.targets.externalDomain</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Domains to resolve and to ping. Make sure the v6 domain resolves both A and AAAA records, while the v4 only resolves A records.</td>
		</tr>
		<tr>
			<td>pinger.targets.externalDomain.v4</td>
			<td>string</td>
			<td><pre lang="json">
"kube-ovn.io."
</pre>
</td>
			<td>Domain name resolving to an IPv4 only (A record)</td>
		</tr>
		<tr>
			<td>pinger.targets.externalDomain.v6</td>
			<td>string</td>
			<td><pre lang="json">
"google.com."
</pre>
</td>
			<td>Domain name resolving to an IPv6 and IPv4 only (A/AAAA record)</td>
		</tr>
	</tbody>
</table>
<h3>Validating webhook configuration</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
		<tr>
			<td>validatingWebhook</td>
			<td>object</td>
			<td><pre lang="">
"{}"
</pre>
</td>
			<td>Configuration of the validating webhook used to verify custom resources before they are pushed to Kubernetes. Make sure cert-manager is installed for the generation of certificates for the webhook. See https://kubeovn.github.io/docs/stable/en/guide/webhook/</td>
		</tr>
		<tr>
			<td>validatingWebhook.annotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to all top-level kube-ovn-webhook objects (resources under templates/webhook)</td>
		</tr>
		<tr>
			<td>validatingWebhook.enabled</td>
			<td>bool</td>
			<td><pre lang="json">
false
</pre>
</td>
			<td>Enable the deployment of the validating webhook.</td>
		</tr>
		<tr>
			<td>validatingWebhook.labels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to all top-level kube-ovn-webhook objects (resources under templates/webhook)</td>
		</tr>
		<tr>
			<td>validatingWebhook.podAnnotations</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Annotations to be added to kube-ovn-webhook pods.</td>
		</tr>
		<tr>
			<td>validatingWebhook.podLabels</td>
			<td>object</td>
			<td><pre lang="json">
{}
</pre>
</td>
			<td>Labels to be added to kube-ovn-webhook pods.</td>
		</tr>
	</tbody>
</table>

<h3>Other Values</h3>
<table>
	<thead>
		<th>Key</th>
		<th>Type</th>
		<th>Default</th>
		<th>Description</th>
	</thead>
	<tbody>
	<tr>
		<td>ovsOvn.ovsIpsecKeysDirectory</td>
		<td>string</td>
		<td><pre lang="json">
"/etc/origin/ovs_ipsec_keys"
</pre>
</td>
		<td>Directory on the node where Open vSwitch (OVS) IPSEC keys live.</td>
	</tr>
	</tbody>
</table>

