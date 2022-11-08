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
kind load docker-image --name kube-ovn kubeovn/test:v1.11.0
kubectl apply -f test/server/test-server.yaml
docker run -d --net=kind kubeovn/test:v1.11.0

# Run test-server analysis tool in one terminal and reload kube-ovn in another terminal
# terminal 1
kubectl exec -it test-client -- ./test-server 100.64.0.1
 
# terminal 2
kubectl ko reload

# Try with different address to test different path.
```

# Test result

ICMP test result:

| Scenario                          | Lost |
|-----------------------------------|------|
| Pod address within same node      | 0    |
| ovn0 address with in same node    | 13   |
| Node address the Pod runs on      | 15   |
| Pod address in another node       | 4    |
| ovn0 address with in another node | 21   |
| Node address of anther node       | 16   |
| Address outside the cluster       | 32   |

TCP test result:

| Scenario                        | Retransmit | Connection Failure | Note             |
|---------------------------------|------------|--------------------|------------------|
| Pod address in another node     | 38         | 1                  |                  |
| Service address                 | 86         | 0                  |                  |
| Address outside the cluster     | 4          | 1                  |                  |
| External visit NodePort address |            |                    | Connection Reset |

## TODO

1. NodePort long connection will be reset which need further investigation.
2. Traffic that go through ovn0 suffers higher lost, and it may be related to internal type port.
3. Replace curl with ab to test high connection concurrency.
4. Need to be tested in large scale cluster where kube-ovn reload might take much longer time.
