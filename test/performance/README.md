# Performance Test

Kube-OVN use [KNB](https://github.com/InfraBuilder/k8s-bench-suite) to test datapath throughput performance and resource consumption.

KNB will test below performance metrics:
- Pod to Pod TCP/UDP bandwidth
- Pod to Service TCP/UDP bandwidth
- CPU consumption
- Memory consumption

## Quick Start

1. Download KNB
```bash
wget https://raw.githubusercontent.com/InfraBuilder/k8s-bench-suite/master/knb
chmod +x knb
```
2. Run basic performance test
```bash
./knb -v -cn {Client NodeName} -sn {Server NodeName} --duration 60  
```
