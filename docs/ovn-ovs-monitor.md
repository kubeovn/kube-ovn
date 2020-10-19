# OVN/OVS Monitor Statistics

This document shows monitor metrics about OVN and OVS.


Type | Metric | Description
---|---|---
OVN_Monitor |  | 
 1 | ovn_status | OVN Health Status. The values are: health(1), unhealth(0).
 2 | ovn_info | This metric provides basic information about OVN. It is always set to 1.
 3 | failed_req_count | The number of failed requests to OVN stack.
 4 | log_file_size | The size of a log file associated with an OVN component.
 5 | db_file_size | The size of a database file associated with an OVN component.
 6 | chassis_info | Whether the OVN chassis is up (1) or down (0), together with additional information about the chassis.
 7 | logical_switch_info | The information about OVN logical switch. This metric is always up (1).
 8 | logical_switch_external_id | Provides the external IDs and values associated with OVN logical switches. This metric is always up (1).
 9 | logical_switch_port_binding | Provides the association between a logical switch and a logical switch port. This metric is always up (1).
 10 | logical_switch_tunnel_key | The value of the tunnel key associated with the logical switch.
 11 | logical_switch_ports_num | The number of logical switch ports connected to the OVN logical switch.
 12 | logical_switch_port_info | The information about OVN logical switch port. This metric is always up (1).
 13 | logical_switch_port_tunnel_key | The value of the tunnel key associated with the logical switch port.
 14 | cluster_enabled | Is OVN clustering enabled (1) or not (0).
 15 | cluster_role | A metric with a constant '1' value labeled by server role.
 16 | cluster_status | A metric with a constant '1' value labeled by server status.
 17 | cluster_term | The current raft term known by this server.
 18 | cluster_leader_self | Is this server consider itself a leader (1) or not (0).
 19 | cluster_vote_self | Is this server voted itself as a leader (1) or not (0).
 20 | cluster_election_timer | The current election timer value.
 21 | cluster_log_not_committed | The number of log entries not yet committed by this server.
 22 | cluster_log_not_applied | The number of log entries not yet applied by this server.
 23 | cluster_log_index_start | The log entry index start value associated with this server.
 24 | cluster_log_index_next | The log entry index next value associated with this server.
 25 | cluster_inbound_connections_total | The total number of inbound connections to the server.
 26 | cluster_outbound_connections_total | The total number of outbound connections from the server.
 27 | cluster_inbound_connections_error_total | The total number of failed inbound connections to the server.
 28 | cluster_outbound_connections_error_total | The total number of failed outbound connections from the server.
OVS_Monitor | | 
 1 | ovs_status | OVS Health Status. The values are: health(1), unhealth(0).
 2 | ovs_info | This metric provides basic information about OVS. It is always set to 1.
 3 | failed_req_count | The number of failed requests to OVS stack.
 4 | log_file_size | The size of a log file associated with an OVS component.
 5 | db_file_size | The size of a database file associated with an OVS component.
 6 | datapath | Represents an existing datapath. This metrics is always 1.
 7 | dp_total | Represents total number of datapaths on the system.
 8 | dp_if | Represents an existing datapath interface. This metrics is always 1.
 9 | dp_if_total | Represents the number of ports connected to the datapath.
 10 | dp_flows_total | The number of flows in a datapath.
 11 | dp_flows_lookup_hit | The number of incoming packets in a datapath matching existing flows in the datapath.
 12 | dp_flows_lookup_missed | The number of incoming packets in a datapath not matching any existing flow in the datapath.
 13 | dp_flows_lookup_lost | The number of incoming packets in a datapath destined for userspace process but subsequently dropped before reaching userspace.
 14 | dp_masks_hit | The total number of masks visited for matching incoming packets.
 15 | dp_masks_total | The number of masks in a datapath.
 16 | dp_masks_hit_ratio | The average number of masks visited per packet. It is the ration between hit and total number of packets processed by a datapath.
 17 | interface | Represents OVS interface. This is the primary metric for all other interface metrics. This metrics is always 1.
 18 | interface_admin_state | The administrative state of the physical network link of OVS interface. The values are: down(0), up(1), other(2).
 19 | interface_link_state | The state of the physical network link of OVS interface. The values are: down(0), up(1), other(2).
 20 | interface_mac_in_use | The MAC address in use by OVS interface.
 21 | interface_mtu | The currently configured MTU for OVS interface.
 22 | interface_of_port | Represents the OpenFlow port ID associated with OVS interface.
 23 | interface_if_index | Represents the interface index associated with OVS interface.
 24 | interface_tx_packets | Represents the number of transmitted packets by OVS interface.
 25 | interface_tx_bytes | Represents the number of transmitted bytes by OVS interface.
 26 | interface_rx_packets | Represents the number of received packets by OVS interface.
 27 | interface_rx_bytes | Represents the number of received bytes by OVS interface.
 28 | interface_rx_crc_err | Represents the number of CRC errors for the packets received by OVS interface.
 29 | interface_rx_dropped | Represents the number of input packets dropped by OVS interface.
 30 | interface_rx_errors | Represents the total number of packets with errors received by OVS interface.
 31 | interface_rx_frame_err | Represents the number of frame alignment errors on the packets received by OVS interface.
 32 | interface_rx_missed_err | Represents the number of packets with RX missed received by OVS interface.
 33 | interface_rx_over_err | Represents the number of packets with RX overrun received by OVS interface.
 34 | interface_tx_dropped | Represents the number of output packets dropped by OVS interface.
 35 | interface_tx_errors | Represents the total number of transmit errors by OVS interface.
 36 | interface_collisions | Represents the number of collisions on OVS interface.