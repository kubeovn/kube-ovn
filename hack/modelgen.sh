#! /usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

OVS_VERSION="3.5"
OVN_VERSION="25.03"

# download vswitch/nb/sb schema files
curl -sSf -L --retry 5 -o vswitch.ovsschema \
    https://raw.githubusercontent.com/openvswitch/ovs/refs/heads/branch-${OVS_VERSION}/vswitchd/vswitch.ovsschema
curl -sSf -L --retry 5 -o ovn-nb.ovsschema \
    https://raw.githubusercontent.com/ovn-org/ovn/refs/heads/branch-${OVN_VERSION}/ovn-nb.ovsschema
curl -sSf -L --retry 5 -o ovn-sb.ovsschema \
    https://raw.githubusercontent.com/ovn-org/ovn/refs/heads/branch-${OVN_VERSION}/ovn-sb.ovsschema

# remove old generated files
rm -rfv pkg/ovsdb/vswitch pkg/ovsdb/ovnnb pkg/ovsdb/ovnsb

# generate go code from vswitch/nb/sb schema files
go tool github.com/ovn-kubernetes/libovsdb/cmd/modelgen \
    -p vswitch -o pkg/ovsdb/vswitch vswitch.ovsschema
go tool github.com/ovn-kubernetes/libovsdb/cmd/modelgen \
    -p ovnnb -o pkg/ovsdb/ovnnb ovn-nb.ovsschema
go tool github.com/ovn-kubernetes/libovsdb/cmd/modelgen \
    -p ovnsb -o pkg/ovsdb/ovnsb ovn-sb.ovsschema

# remove downloaded schema files
rm -fv vswitch.ovsschema ovn-nb.ovsschema ovn-sb.ovsschema

# add generated files to git
git add pkg/ovsdb/vswitch pkg/ovsdb/ovnnb pkg/ovsdb/ovnsb
