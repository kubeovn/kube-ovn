#!/bin/bash
set -eo pipefail

TS_NAME=${TS_NAME:-ts}
TS_CIDR=${TS_CIDR:-169.254.100.0/24}

/usr/share/ovn/scripts/ovn-ctl --db-ic-nb-create-insecure-remote=yes --db-ic-sb-create-insecure-remote=yes start_ic_ovsdb
/usr/share/ovn/scripts/ovn-ctl status_ic_ovsdb
ovn-ic-nbctl ts-add "$TS_NAME"
ovn-ic-nbctl set Transit_Switch ts external_ids:subnet="$TS_CIDR"
tail -f /var/log/ovn/ovsdb-server-ic-nb.log
