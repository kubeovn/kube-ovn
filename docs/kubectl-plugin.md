Kube-OVN provides a kubectl plugin to help better diagnose container network. You can use this plugin to tcpdump a specific pod, trace a specific packet or query ovn-nb/ovn-sb.

# Prerequisite

To enable kubectl plugin, kubectl version of 1.12 or later is recommended. You can use `kubectl version` to check the version.

# Install

1. Get the `kubectl-ko` file
```bash
wget https://raw.githubusercontent.com/alauda/kube-ovn/master/dist/images/kubectl-ko
```

2. Move the file to one of $PATH directories
```bash
mv kubectl-ko /usr/local/bin/kubectl-ko
```

3. Add executable permission to `kubectl-ko`
```bash
chmod +x /usr/local/bin/kubectl-ko
```

4. Check if the plugin is ready
```bash
[root@kube-ovn01 ~]# kubectl plugin list
The following compatible plugins are available:

/usr/local/bin/kubectl-ko
```

# Usage

```bash
kubectl ko {subcommand} [option...]
Available Subcommands:
  nbctl [ovn-nbctl options ...]    invoke ovn-nbctl
  sbctl [ovn-sbctl options ...]    invoke ovn-sbctl
  tcpdump {namespace/podname} [tcpdump options ...] capture pod traffic
  trace {namespace/podname} {target ip address} {icmp|tcp|udp} [target tcp or udp port]
  diagnose {all|node} [nodename]    diagnose connectivity of all nodes or a specific node
```
