.PHONY: build-dev-images build-go

build-dev-images: build-go
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-node:dev -f dist/images/Dockerfile.node dist/images/
	docker push index.alauda.cn/alaudak8s/kube-ovn-node:dev
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-controller:dev -f dist/images/Dockerfile.controller dist/images/
	docker push index.alauda.cn/alaudak8s/kube-ovn-controller:dev
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-cni:dev -f dist/images/Dockerfile.cni dist/images/
	docker push index.alauda.cn/alaudak8s/kube-ovn-cni:dev

build-go:
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn -ldflags "-w -s" -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-controller -ldflags "-w -s" -v ./cmd/controller
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-daemon -ldflags "-w -s" -v ./cmd/daemon

release: build-go
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-node:`cat VERSION` -f dist/images/Dockerfile.node dist/images/
	docker push index.alauda.cn/alaudak8s/kube-ovn-node:`cat VERSION`
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-controller:`cat VERSION` -f dist/images/Dockerfile.controller dist/images/
	docker push index.alauda.cn/alaudak8s/kube-ovn-controller:`cat VERSION`
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-cni:`cat VERSION` -f dist/images/Dockerfile.cni dist/images/
	docker push index.alauda.cn/alaudak8s/kube-ovn-cni:`cat VERSION`