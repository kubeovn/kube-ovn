#!/bin/bash
set -eux
# usage: bash ./hack/update-codegen-crd.sh

# set GOPROXY you like
export GOPROXY=https://goproxy.cn
# use controller-gen to generate CRDs
# ensure controller-gen is installed
CONTROLLER_TOOLS_VERSION=${CONTROLLER_TOOLS_VERSION:-"v0.17.3"}
go install sigs.k8s.io/controller-tools/cmd/controller-gen@"${CONTROLLER_TOOLS_VERSION}"
go mod tidy

# generate CRDs
controller-gen crd:allowDangerousTypes=true paths=./pkg/apis/kubeovn/v1 output:crd:artifacts:config=./yamls/crds
