set -o errexit
set -o nounset
set -o pipefail
set -x

SCRIPT_ROOT=$(dirname ${BASH_SOURCE})/..
CODEGEN_PKG="/usr/local/pkg/mod/k8s.io/code-generator@v0.32.6"
MODULE=github.com/kubeovn/kube-ovn

# generate the code with:
# --output-base    because this script should also be able to run inside the vendor dir of
#                  k8s.io/kubernetes. The output-base is needed for the generators to output into the vendor dir
#                  instead of the $GOPATH directly. For normal projects this can be dropped.
# ${CODEGEN_PKG}/generate-groups.sh "deepcopy,client,informer,lister" \
#   github.com/kubeovn/kube-ovn/pkg/client github.com/kubeovn/kube-ovn/pkg/apis \
#   kubeovn:v1
${CODEGEN_PKG}/kube_codegen.sh "deepcopy,client,informer,lister" \
  github.com/kubeovn/kube-ovn/pkg/client github.com/kubeovn/kube-ovn/pkg/apis \
  kubeovn:v1
