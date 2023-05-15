# Development Guide

## How to build Kube-OVN

##### Prerequisites:

1. Kube-OVN is developed by [Go](https://golang.org/) 1.20 and uses [Go Modules](https://github.com/golang/go/wiki/Modules) to manage dependency. Make sure `GO111MODULE="on"`.

2. We also use [gosec](https://github.com/securego/gosec) to inspect source code for security problems. 

```shell
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

3. To minimize image size we use docker experimental buildx features.

​	For version < Docker 19.03, please enable it manually through the [reference](https://docs.docker.com/develop/develop-images/build_enhancements/).

​    Buildx could also be installed from official [doc](https://github.com/docker/buildx/).

​	For first compilation, create a new builder instance.

```shell
docker buildx create --use
```

##### Make:

```shell
git clone https://github.com/kubeovn/kube-ovn.git
go install github.com/securego/gosec/v2/cmd/gosec@latest
cd kube-ovn
make release
```

## How to run e2e tests

Kube-OVN uses [KIND](https://kind.sigs.k8s.io/) to setup a local Kubernetes cluster and [j2cli](https://github.com/kolypto/j2cli) to render template
and [Ginkgo](https://onsi.github.io/ginkgo/) as the test framework to run the e2e tests.

```shell
make kind-init
make kind-install
# wait all pods ready
go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo
make e2e
```

For Underlay mode e2e tests with single nic, run following commands:

```sh
make kind-init
make kind-install-underlay
# wait all pods ready
make e2e-underlay-single-nic
```

## ARM support

If you want to run Kube-OVN on arm64 platform, you need to build the arm64 images with docker multi-platform build.

```bash
make release-arm
```
