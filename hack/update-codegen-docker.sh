# use docker to generate code
# useage: bash ./hack/update-codegen-docker.sh

# set GOPROXY you like
GOPROXY="https://goproxy.cn"

PROJECT_PACKAGE=github.com/kubeovn/kube-ovn
docker run -it --rm \
    -v ${PWD}:/go/src/${PROJECT_PACKAGE}\
    -v ${PWD}/hack/boilerplate.go.txt:/tmp/fake-boilerplate.txt \
    -e PROJECT_PACKAGE=${PROJECT_PACKAGE} \
    -e CLIENT_GENERATOR_OUT=${PROJECT_PACKAGE}/pkg/client \
    -e APIS_ROOT=${PROJECT_PACKAGE}/pkg/apis \
    -e GROUPS_VERSION="kubeovn:v1" \
    -e GENERATION_TARGETS="deepcopy,client,informer,lister" \
    -e GOPROXY=${GOPROXY} \
    quay.io/slok/kube-code-generator:v1.26.0
