#!/bin/bash
# usage: bash -x ./hack/update-codegen-crd.sh
set -eux
cd "$(dirname "$0")/.."

# set GOPROXY you like
export GOPROXY=${GOPROXY:-"https://goproxy.cn"}
# use controller-gen to generate CRDs
# ensure controller-gen is installed
CONTROLLER_TOOLS_VERSION=${CONTROLLER_TOOLS_VERSION:-"v0.19.0"}
go install sigs.k8s.io/controller-tools/cmd/controller-gen@"${CONTROLLER_TOOLS_VERSION}"
go mod tidy

# generate CRDs
controller-gen crd:allowDangerousTypes=true paths=./pkg/apis/kubeovn/v1 output:crd:artifacts:config=./yamls/crds
