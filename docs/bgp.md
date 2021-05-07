# BGP support

Kube-OVN supports advertise pod/subnet ips to external networks by BGP protocol.
To enable BGP advertise function, you need to install kube-ovn-speaker and annotate pods/subnets that need to be exposed.

## List of Options
```text
Usage of ovn-speaker:
      --add_dir_header                   If true, adds the file directory to the header
      --alsologtostderr                  log to standard error as well as files
      --auth-password string             bgp peer auth password
      --cluster-as uint32                The as number of container network, default 65000 (default 65000)
      --grpc-host string                 The host address for grpc to listen, default: 127.0.0.1 (default "127.0.0.1")
      --grpc-port uint32                 The port for grpc to listen, default:50051 (default 50051)
      --holdtime duration                ovn-speaker goes down abnormally, the local saving time of BGP route will be affected.Holdtime must be in the range 3s to 65536s. (default 90s) (default 1m30s)
      --kubeconfig string                Path to kubeconfig file with authorization and master location information. If not set use the inCluster token.
      --log_backtrace_at traceLocation   when logging hits line file:N, emit a stack trace (default :0)
      --log_dir string                   If non-empty, write log files in this directory
      --log_file string                  If non-empty, use this log file
      --log_file_max_size uint           Defines the maximum size a log file can grow to. Unit is megabytes. If the value is 0, the maximum file size is unlimited. (default 1800)
      --logtostderr                      log to standard error instead of files (default true)
      --neighbor-address string          The router address the speaker connects to.
      --neighbor-as uint32               The router as number, default 65001 (default 65001)
      --pprof-port uint32                The port to get profiling data, default: 10667 (default 10667)
      --router-id string                 The address for the speaker to use as router id, default the node ip
      --skip_headers                     If true, avoid header prefixes in the log messages
      --skip_log_headers                 If true, avoid headers when opening log files
      --stderrthreshold severity         logs at or above this threshold go to stderr (default 2)
  -v, --v Level                          number for the log level verbosity
      --vmodule moduleSpec               comma-separated list of pattern=N settings for file-filtered logging

```

1. Label nodes that host the BGP speaker and act as overlay to underlay gateway
```bash
kubectl label nodes speaker-node-1 ovn.kubernetes.io/bgp=true
kubectl label nodes speaker-node-2 ovn.kubernetes.io/bgp=true
```

## Install kube-ovn-speaker

2. Download `kube-ovn-speaker` yaml

```bash
wget https://github.com/kubeovn/kube-ovn/blob/master/yamls/speaker.yaml
```

3. Modify the args in yaml

```bash
--neighbor-address=10.32.32.1  # The router address that need to establish bgp peers
--neighbor-as=65030            # The AS of router
--cluster-as=65000             # The AS of container network
```

4. Apply the yaml

```bash
kubectl apply -f speaker.yaml
```


*NOTE*: When more than one node host speaker, the upstream router need to support multiple path routes to act ECMP.

## Annotate pods/subnet that need to be exposed

The subnet of pods and subnets need to be advertised should set `natOutgoing` to `false`

```bash
# Enable BGP advertise
kubectl annotate pod sample ovn.kubernetes.io/bgp=true
kubectl annotate subnet ovn-default ovn.kubernetes.io/bgp=true

# Disable BGP advertise
kubectl annotate pod perf-ovn-xzvd4 ovn.kubernetes.io/bgp-
kubectl annotate subnet ovn-default ovn.kubernetes.io/bgp-
```
