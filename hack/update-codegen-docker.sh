# use docker to generate code
# useage: bash ./hack/update-codegen-docker.sh

# set GOPROXY you like
GOPROXY="https://goproxy.cn"

docker run -it --rm \
    -v ${PWD}:/app \
    -e GOPROXY=${GOPROXY} \
    ghcr.io/zhangzujian/kube-code-generator:v0.2.1 \
    --boilerplate-path ./hack/boilerplate.go.txt \
    --apis-in ./pkg/apis \
    --go-gen-out ./pkg/client

go mod tidy
