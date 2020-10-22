# OVSDB Management Protocol (RFC 7047) Client Library

This Go library implements a client for the [Open vSwitch Database Management
Protocol](https://tools.ietf.org/html/rfc7047).

## Overview

The client can be used for communicating with:
* Open vSwitch database
* OVN Northbound and Southbound databases
* Application services

The following tables describe the implementation state for the protocol's RPC
methods and database operations.

| **RFC Section** | **Method** | **Implemented?** |
| --- | --- | --- |
| 4.1.1. | List Databases (`list_dbs`) | :white_check_mark: |
| 4.1.2. | Get Schema (`get_schema`) | :white_check_mark: |
| 4.1.3. | Transact (`transact`) | :white_check_mark: :warning: `select` operation only |
| 4.1.4. | Cancel  | :white_medium_square: |
| 4.1.5. | Monitor  | :white_medium_square: |
| 4.1.6. | Update Notification  | :white_medium_square: |
| 4.1.7. | Monitor Cancellation  | :white_medium_square: |
| 4.1.8. | Lock Operations  | :white_medium_square: |
| 4.1.9. | Locked Notification  | :white_medium_square: |
| 4.1.10. | Stolen Notification  |  :white_medium_square: |
| 4.1.11. | Echo (`echo`)| :white_check_mark: |

| **RFC Section** | **Operation** | **Implemented?** |
| --- | --- | --- |
| 5.2.1. | Insert | :white_medium_square: |
| 5.2.2. | Select | :white_check_mark: |
| 5.2.3. | Update | :white_medium_square: |
| 5.2.4. | Mutate | :white_medium_square: |
| 5.2.5. | Delete | :white_medium_square: |
| 5.2.6. | Wait | :white_medium_square: |
| 5.2.7. | Commit | :white_medium_square: |
| 5.2.8. | Abort | :white_medium_square: |
| 5.2.9. | Comment | :white_medium_square: |
| 5.2.10. | Assert | :white_medium_square: |

Additionally, the library implements the following application calls:
* `list-commands`
* `cluster/status`
* `coverage/show`

The goals of the [`OWNERS`](OWNERS) is:
* implementing all methods and operations described in the RPC
* documenting all the implemented methods and operations
* achieving and maintaining test coverage of 80% or higher, and
* providing ongoing support

There are alternatives to this client library. The following list contains
notable libraries written in Go:

* [digitalocean/go-openvswitch](https://github.com/digitalocean/go-openvswitch)
* [socketplane/libovsdb](https://github.com/socketplane/libovsdb)
* [eBay/go-ovn](https://github.com/eBay/go-ovn)

Currently, the best example how to use the library
is [OVN Exporter for Prometheus](https://github.com/greenpau/ovn_exporter/).
