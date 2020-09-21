Pinger makes network requests between pods/nodes/services/dns to test the connectivity in the cluster and expose metrics in Prometheus format.

## Prometheus Integration

Pinger exposes metrics at `:8080/metrics`, it will show following metrics

```bash
pinger_ovs_up
pinger_ovs_down
pinger_ovn_controller_up
pinger_ovn_controller_down
pinger_dns_healthy
pinger_dns_unhealthy
pinger_dns_latency_ms
pinger_pod_ping_latency_ms
pinger_pod_ping_lost_total
pinger_node_ping_latency_ms
pinger_node_ping_lost_total
```

Kube-OVN-Controller expose metrics at `10660/metrics`, it will show controller runtime metrics.

You can use kube-prometheus to scrape the metrics. The related ServiceMonitor yaml can be found [here](../dist/monitoring)

## Grafana Dashboard

Pinger grafana dashboard config can be found [here](../dist/monitoring/pinger-grafana.json).

![alt text](pinger-grafana.png "kube-ovn-pinger grafana dashboard")


Kube-OVN-Controller grafana dashboard config can be found [here](../dist/monitoring/controller-grafana.json)

![alt text](controller-grafana.png "kube-ovn-controller grafana dashboard")

Kube-OVN-CNI grafana dashboard config can be found [here](../dist/monitoring/cni-grafana.json)
![alt text](cni-grafana.png "kube-ovn-controller grafana dashboard")
