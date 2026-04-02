#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."

CONTROLLER_GEN_BIN="${GOBIN:-$(go env GOPATH)/bin}/controller-gen"
CONTROLLER_TOOLS_VERSION=${CONTROLLER_TOOLS_VERSION:-"v0.20.1"}
go install sigs.k8s.io/controller-tools/cmd/controller-gen@"${CONTROLLER_TOOLS_VERSION}"

# Clear old generated crds to avoid duplicate files
mkdir -p ./yamls/gen
rm -f ./yamls/gen/*.yaml

"${CONTROLLER_GEN_BIN}" crd:allowDangerousTypes=true paths=./pkg/apis/kubeovn/v1 output:crd:artifacts:config=./yamls/gen
