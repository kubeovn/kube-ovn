# Kube-OVN Monitor Metrics

This document shows Kube-OVN monitor metrics.

| Type                | Metric                                   | Description                                                                                                                       |
| ------------------- | ---------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------- |
| OVN_Monitor         |                                          | OVN NB/SB/Northd metrics                                                                                                          |
| Gauge               | ovn_status                               | OVN Health Status. The values are: (2) for standby or follower, (1) for active or leader, (0) for unhealthy.                                                                        |
| Gauge               | ovn_info                                 | This metric provides basic information about OVN. It is always set to 1.                                                          |
| Gauge               | failed_req_count                         | The number of failed requests to OVN stack.                                                                                       |
| Gauge               | log_file_size                            | The size of a log file associated with an OVN component.                                                                          |
| Gauge               | db_file_size                             | The size of a database file associated with an OVN component.                                                                     |
| Gauge               | chassis_info                             | Whether the OVN chassis is up (1) or down (0), together with additional information about the chassis.                            |
| Gauge               | db_status                                | The status of OVN NB/SB DB, (1) for healthy, (0) for unhealthy.                                                                   |
| Gauge               | logical_switch_info                      | The information about OVN logical switch. This metric is always up (1).                                                           |
| Gauge               | logical_switch_external_id               | Provides the external IDs and values associated with OVN logical switches. This metric is always up (1).                          |
| Gauge               | logical_switch_port_binding              | Provides the association between a logical switch and a logical switch port. This metric is always up (1).                        |
| Gauge               | logical_switch_tunnel_key                | The value of the tunnel key associated with the logical switch.                                                                   |
| Gauge               | logical_switch_ports_num                 | The number of logical switch ports connected to the OVN logical switch.                                                           |
| Gauge               | logical_switch_port_info                 | The information about OVN logical switch port. This metric is always up (1).                                                      |
| Gauge               | logical_switch_port_tunnel_key           | The value of the tunnel key associated with the logical switch port.                                                              |
| Gauge               | cluster_enabled                          | Is OVN clustering enabled (1) or not (0).                                                                                         |
| Gauge               | cluster_role                             | A metric with a constant '1' value labeled by server role.                                                                        |
| Gauge               | cluster_status                           | A metric with a constant '1' value labeled by server status.                                                                      |
| Gauge               | cluster_term                             | The current raft term known by this server.                                                                                       |
| Gauge               | cluster_leader_self                      | Is this server consider itself a leader (1) or not (0).                                                                           |
| Gauge               | cluster_vote_self                        | Is this server voted itself as a leader (1) or not (0).                                                                           |
| Gauge               | cluster_election_timer                   | The current election timer value.                                                                                                 |
| Gauge               | cluster_log_not_committed                | The number of log entries not yet committed by this server.                                                                       |
| Gauge               | cluster_log_not_applied                  | The number of log entries not yet applied by this server.                                                                         |
| Gauge               | cluster_log_index_start                  | The log entry index start value associated with this server.                                                                      |
| Gauge               | cluster_log_index_next                   | The log entry index next value associated with this server.                                                                       |
| Gauge               | cluster_inbound_connections_total        | The total number of inbound connections to the server.                                                                            |
| Gauge               | cluster_outbound_connections_total       | The total number of outbound connections from the server.                                                                         |
| Gauge               | cluster_inbound_connections_error_total  | The total number of failed inbound connections to the server.                                                                     |
| Gauge               | cluster_outbound_connections_error_total | The total number of failed outbound connections from the server.                                                                  |
| OVS_Monitor         |                                          | ovsdb/vswitchd metrics                                                                                                            |
| Gauge               | ovs_status                               | OVS Health Status. The values are: health(1), unhealthy(0).                                                                        |
| Gauge               | ovs_info                                 | This metric provides basic information about OVS. It is always set to 1.                                                          |
| Gauge               | failed_req_count                         | The number of failed requests to OVS stack.                                                                                       |
| Gauge               | log_file_size                            | The size of a log file associated with an OVS component.                                                                          |
| Gauge               | db_file_size                             | The size of a database file associated with an OVS component.                                                                     |
| Gauge               | datapath                                 | Represents an existing datapath. This metrics is always 1.                                                                        |
| Gauge               | dp_total                                 | Represents total number of datapaths on the system.                                                                               |
| Gauge               | dp_if                                    | Represents an existing datapath interface. This metrics is always 1.                                                              |
| Gauge               | dp_if_total                              | Represents the number of ports connected to the datapath.                                                                         |
| Gauge               | dp_flows_total                           | The number of flows in a datapath.                                                                                                |
| Gauge               | dp_flows_lookup_hit                      | The number of incoming packets in a datapath matching existing flows in the datapath.                                             |
| Gauge               | dp_flows_lookup_missed                   | The number of incoming packets in a datapath not matching any existing flow in the datapath.                                      |
| Gauge               | dp_flows_lookup_lost                     | The number of incoming packets in a datapath destined for userspace process but subsequently dropped before reaching userspace.   |
| Gauge               | dp_masks_hit                             | The total number of masks visited for matching incoming packets.                                                                  |
| Gauge               | dp_masks_total                           | The number of masks in a datapath.                                                                                                |
| Gauge               | dp_masks_hit_ratio                       | The average number of masks visited per packet. It is the ration between hit and total number of packets processed by a datapath. |
| Gauge               | interface                                | Represents OVS interface. This is the primary metric for all other interface metrics. This metrics is always 1.                   |
| Gauge               | interface_admin_state                    | The administrative state of the physical network link of OVS interface. The values are: down(0), up(1), other(2).                 |
| Gauge               | interface_link_state                     | The state of the physical network link of OVS interface. The values are: down(0), up(1), other(2).                                |
| Gauge               | interface_mac_in_use                     | The MAC address in use by OVS interface.                                                                                          |
| Gauge               | interface_mtu                            | The currently configured MTU for OVS interface.                                                                                   |
| Gauge               | interface_of_port                        | Represents the OpenFlow port ID associated with OVS interface.                                                                    |
| Gauge               | interface_if_index                       | Represents the interface index associated with OVS interface.                                                                     |
| Gauge               | interface_tx_packets                     | Represents the number of transmitted packets by OVS interface.                                                                    |
| Gauge               | interface_tx_bytes                       | Represents the number of transmitted bytes by OVS interface.                                                                      |
| Gauge               | interface_rx_packets                     | Represents the number of received packets by OVS interface.                                                                       |
| Gauge               | interface_rx_bytes                       | Represents the number of received bytes by OVS interface.                                                                         |
| Gauge               | interface_rx_crc_err                     | Represents the number of CRC errors for the packets received by OVS interface.                                                    |
| Gauge               | interface_rx_dropped                     | Represents the number of input packets dropped by OVS interface.                                                                  |
| Gauge               | interface_rx_errors                      | Represents the total number of packets with errors received by OVS interface.                                                     |
| Gauge               | interface_rx_frame_err                   | Represents the number of frame alignment errors on the packets received by OVS interface.                                         |
| Gauge               | interface_rx_missed_err                  | Represents the number of packets with RX missed received by OVS interface.                                                        |
| Gauge               | interface_rx_over_err                    | Represents the number of packets with RX overrun received by OVS interface.                                                       |
| Gauge               | interface_tx_dropped                     | Represents the number of output packets dropped by OVS interface.                                                                 |
| Gauge               | interface_tx_errors                      | Represents the total number of transmit errors by OVS interface.                                                                  |
| Gauge               | interface_collisions                     | Represents the number of collisions on OVS interface.                                                                             |
| Kube-OVN-Pinger     |                                          | Network quality metrics                                                                                                           |
| Gauge               | pinger_ovs_up                            | If the ovs on the node is up                                                                                                      |
| Gauge               | pinger_ovs_down                          | If the ovs on the node is down                                                                                                    |
| Gauge               | pinger_ovn_controller_up                 | If the ovn_controller on the node is up                                                                                           |
| Gauge               | pinger_ovn_controller_down               | If the ovn_controller on the node is down                                                                                         |
| Gauge               | pinger_inconsistent_port_binding         | The number of mismatch port bindings between ovs and ovn-sb                                                                       |
| Gauge               | pinger_apiserver_healthy                 | If the apiserver request is healthy on this node                                                                                  |
| Gauge               | pinger_apiserver_unhealthy               | If the apiserver request is unhealthy on this node                                                                                |
| Histogram           | pinger_apiserver_latency_ms              | The latency ms histogram the node request apiserver                                                                               |
| Gauge               | pinger_internal_dns_healthy              | If the internal dns request is unhealthy on this node                                                                             |
| Gauge               | pinger_internal_dns_unhealthy            | If the internal dns request is unhealthy on this node                                                                             |
| Histogram           | pinger_internal_dns_latency_ms           | The latency ms histogram the node request internal dns                                                                            |
| Gauge               | pinger_external_dns_health               | If the external dns request is healthy on this node                                                                               |
| Gauge               | pinger_external_dns_unhealthy            | If the external dns request is unhealthy on this node                                                                             |
| Histogram           | pinger_external_dns_latency_ms           | The latency ms histogram the node request external dns                                                                            |
| Histogram           | pinger_pod_ping_latency_ms               | The latency ms histogram for pod peer ping                                                                                        |
| Gauge               | pinger_pod_ping_lost_total               | The lost count for pod peer ping                                                                                                  |
| Gauge               | pinger_pod_ping_count_total              | The total count for pod peer ping                                                                                                 |
| Histogram           | pinger_node_ping_latency_ms              | The latency ms histogram for pod ping node                                                                                        |
| Gauge               | pinger_node_ping_lost_total              | The lost count for pod ping node                                                                                                  |
| Gauge               | pinger_node_ping_count_total             | The total count for pod ping node                                                                                                 |
| Histogram           | pinger_external_ping_latency_ms          | The latency ms histogram for pod ping external address                                                                            |
| Gauge               | pinger_node_external_lost_total          | The lost count for pod ping external address                                                                                      |
| Kube-OVN-Controller |                                          | Controller metrics                                                                                                                |
| Histogram           | rest_client_request_latency_seconds      | Request latency in seconds. Broken down by verb and URL                                                                           |
| Counter             | rest_client_requests_total               | Number of HTTP requests, partitioned by status code, method, and host                                                             |
| Counter             | lists_total                              | Total number of API lists done by the reflectors                                                                                  |
| Summary             | list_duration_seconds                    | How long an API list takes to return and decode for the reflectors                                                                |
| Summary             | items_per_list                           | How many items an API list returns to the reflectors                                                                              |
| Counter             | watches_total                            | Total number of API watches done by the reflectors                                                                                |
| Counter             | short_watches_total                      | Total number of short API watches done by the reflectors                                                                          |
| Summary             | watch_duration_seconds                   | How long an API watch takes to return and decode for the reflectors                                                               |
| Summary             | items_per_watch                          | How many items an API watch returns to the reflectors                                                                             |
| Gauge               | last_resource_version                    | Last resource version seen for the reflectors                                                                                     |
| Histogram           | ovs_client_request_latency_milliseconds  | The latency histogram for ovs request                                                                                             |
| Gauge               | subnet_available_ip_count                | The available num of ip address in subnet                                                                                         |
| Gauge               | subnet_used_ip_count                     | The used num of ip address in subnet                                                                                              |
| Kube-OVN-CNI        |                                          | CNI metrics                                                                                                                       |
| Histogram           | cni_op_latency_seconds                   | The latency seconds for cni operations                                                                                            |
| Counter             | cni_wait_address_seconds_total           | Latency that cni wait controller to assign an address                                                                             |
| Counter             | cni_wait_connectivity_seconds_total      | Latency that cni wait address ready in overlay network                                                                            |
| Counter             | cni_wait_route_seconds_total             | Latency that cni wait controller to add routed annotation to pod                                                                  |
| Histogram           | rest_client_request_latency_seconds      | Request latency in seconds. Broken down by verb and URL                                                                           |
| Counter             | rest_client_requests_total               | Number of HTTP requests, partitioned by status code, method, and host                                                             |
| Counter             | lists_total                              | Total number of API lists done by the reflectors                                                                                  |
| Summary             | list_duration_seconds                    | How long an API list takes to return and decode for the reflectors                                                                |
| Summary             | items_per_list                           | How many items an API list returns to the reflectors                                                                              |
| Counter             | watches_total                            | Total number of API watches done by the reflectors                                                                                |
| Counter             | short_watches_total                      | Total number of short API watches done by the reflectors                                                                          |
| Summary             | watch_duration_seconds                   | How long an API watch takes to return and decode for the reflectors                                                               |
| Summary             | items_per_watch                          | How many items an API watch returns to the reflectors                                                                             |
| Gauge               | last_resource_version                    | Last resource version seen for the reflectors                                                                                     |
| Histogram           | ovs_client_request_latency_milliseconds  | The latency histogram for ovs request                                                                                             |
