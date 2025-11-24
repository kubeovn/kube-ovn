#!/usr/bin/env bash

set -ex

# setting this env prevents ginkgo e2e from trying to run provider setup
export KUBERNETES_CONFORMANCE_TEST=y

pushd ./test/cnp
go mod download
go test -timeout=0 -v -kubeconfig ${KUBECONFIG}
popd
