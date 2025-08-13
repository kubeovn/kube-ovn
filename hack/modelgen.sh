#! /usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

OVN_VERSION="25.03"

# download ovn nb/sb schema files
curl -sSf -L --retry 5 -o ovn-nb.ovsschema \
    https://raw.githubusercontent.com/ovn-org/ovn/refs/heads/branch-${OVN_VERSION}/ovn-nb.ovsschema
curl -sSf -L --retry 5 -o ovn-sb.ovsschema \
    https://raw.githubusercontent.com/ovn-org/ovn/refs/heads/branch-${OVN_VERSION}/ovn-sb.ovsschema

# remove old generated files
rm -rf pkg/ovsdb/ovnnb pkg/ovsdb/ovnsb

# generate go code from ovn nb/sb schema files
go tool github.com/ovn-kubernetes/libovsdb/cmd/modelgen \
    -p ovnnb -o pkg/ovsdb/ovnnb ovn-nb.ovsschema
go tool github.com/ovn-kubernetes/libovsdb/cmd/modelgen \
    -p ovnsb -o pkg/ovsdb/ovnsb ovn-sb.ovsschema

# remove downloaded schema files
rm -f ovn-nb.ovsschema ovn-sb.ovsschema

# add generated files to git
git add pkg/ovsdb/ovnnb pkg/ovsdb/ovnsb
