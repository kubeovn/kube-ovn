# Test Server

This server mainly focuses on test network break effect during kube-ovn upgrade or restart, but can also be extended to test network connectivity.

## How test server test network break

The test-server will use ping, iperf3 and curl to visit a specified address during upgrade or reload. Then it automatically collect metrics from 
`/proc/net/snmp` and return code to calculate ICMP lost, TCP retransmit packets and TCP connection failure.

```bash
# Deploy a kubernetes cluster with kube-ovn
make kind-init kind-install

# Build and deploy test-server
make image-test
kind load docker-image --name kube-ovn kubeovn/test:v1.12.0
kubectl apply -f test/server/test-server.yaml
docker run --name kube-ovn-test -d --net=kind kubeovn/test:v1.12.0
docker inspect kube-ovn-test -f '{{.NetworkSettings.Networks.kind.IPAddress}}'

# Run test-server analysis tool in one terminal and reload kube-ovn in another terminal
# terminal 1 (replace 172.18.0.5/80 with the address/port you want to test)
kubectl exec -it test-client -- ./test-server --remote-address=172.18.0.5 --remote-port=80 --output=json --duration-seconds=60
 
# terminal 2
kubectl ko reload

# Try with different address to test different path.
```

# Test result

ICMP test result:

| Scenario                          | Lost |
| --------------------------------- | ---- |
| Pod address within same node      | 0    |
| ovn0 address with in same node    | 0    |
| Node address the Pod runs on      | 1    |
| Pod address in another node       | 0    |
| ovn0 address with in another node | 0    |
| Node address of another node      | 0    |
| Address outside the cluster       | 0    |

TCP test result:

| Scenario                        | Retransmit | Connection Failure | Note |
| ------------------------------- | ---------- | ------------------ | ---- |
| Pod address in another node     | 8          | 0                  |      |
| Service address                 | 16         | 0                  |      |
| Address outside the cluster     | 5          | 0                  |      |
| External visit NodePort address | 0          | 0                  |      |

## TODO

1. Replace curl with ab to test high connection concurrency.
2. Need to be tested in large scale cluster where kube-ovn reload might take much longer time.
