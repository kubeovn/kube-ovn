# Development Guide

## How to build Kube-OVN

Kube-OVN is developed by [Go](https://golang.org/) 1.15 and uses [Go Modules](https://github.com/golang/go/wiki/Modules) to manage dependency.

To minimize image size we use docker experiment buildx features, please enable it through the [reference](https://docs.docker.com/develop/develop-images/build_enhancements/).

We also use [gosec](https://github.com/securego/gosec) to inspects source code for security problems.
```
git clone https://github.com/alauda/kube-ovn.git
go get -u github.com/securego/gosec/cmd/gosec
cd kube-ovn
make release
```

## How to run e2e tests

Kube-OVN uses [KIND](https://kind.sigs.k8s.io/) to setup a local Kubernetes cluster and [j2cli](https://github.com/kolypto/j2cli) to render template 
and [Ginkgo](https://onsi.github.io/ginkgo/) as the test framework to run the e2e tests.

```
go get -u github.com/onsi/ginkgo/ginkgo
go get -u github.com/onsi/gomega/...

make kind-init
# wait all pod ready
make e2e
```

## ARM support

If you want to run Kube-OVN on arm64 platform, you need to build the arm64 images with docker multi-platform build.

```bash
make release-arm
```

