.PHONY: build-dev-images build-go

PWD=$(shell pwd)

.ONESHELL:
build-dev-images: build-go
	cd dist/images
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-node:dev -f Dockerfile.node .
	docker push index.alauda.cn/alaudak8s/kube-ovn-node:dev
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-controller:dev -f Dockerfile.controller .
	docker push index.alauda.cn/alaudak8s/kube-ovn-controller:dev
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-cni:dev -f Dockerfile.cni .
	docker push index.alauda.cn/alaudak8s/kube-ovn-cni:dev

.ONESHELL:
build-go:
	CGO_ENABLED=0 GOOS=linux go build -o ${PWD}/dist/images/kube-ovn -ldflags "-w -s" -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux go build -o ${PWD}/dist/images/kube-ovn-controller -ldflags "-w -s" -v ./cmd/controller
	CGO_ENABLED=0 GOOS=linux go build -o ${PWD}/dist/images/kube-ovn-daemon -ldflags "-w -s" -v ./cmd/daemon
