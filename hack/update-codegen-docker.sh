# use docker to generate code
# useage: bash ./hack/update-codegen-docker.sh

# set GOPROXY you like
GOPROXY=${GOPROXY:-"https://goproxy.cn"}

PROJECT_PACKAGE=github.com/kubeovn/kube-ovn
docker run -it --rm \
    -v ${PWD}:/go/src/${PROJECT_PACKAGE}\
    -v ${PWD}/hack/boilerplate.go.txt:/tmp/fake-boilerplate.txt \
    -e PROJECT_PACKAGE=${PROJECT_PACKAGE} \
    -e CLIENT_GENERATOR_OUT=${PROJECT_PACKAGE}/pkg/client \
    -e APIS_ROOT=${PROJECT_PACKAGE}/pkg/apis \
    -e GOPROXY=${GOPROXY} \
    ghcr.io/zhangzujian/kube-code-generator:v1.29.3

go mod tidy
