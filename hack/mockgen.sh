#! /usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# require mockgen v1.6.0+
go generate ./mocks