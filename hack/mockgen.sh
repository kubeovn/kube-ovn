#! /usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

# require go.uber.org/mock/mockgen
go generate ./mocks