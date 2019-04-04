GOFILES_NOVENDOR=$(shell find . -type f -name '*.go' -not -path "./vendor/*")
GO_VERSION=1.12

.PHONY: build-dev-images build-go build-bin test lint

build-dev-images: build-bin
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

lint:
	@gofmt -d ${GOFILES_NOVENDOR} 
	@gofmt -l ${GOFILES_NOVENDOR} | read && echo "Code differs from gofmt's style" 1>&2 && exit 1 || true
	@GOOS=linux go vet ./...

test:
	go test -cover -v ./...

build-bin: lint
	docker run -e GOOS=linux -e GOCACHE=/tmp \
		-u $(shell id -u):$(shell id -g) \
		-v $(CURDIR):/go/src/github.com/alauda/kube-ovn:ro \
		-v $(CURDIR)/dist:/go/src/github.com/alauda/kube-ovn/dist/ \
		golang:$(GO_VERSION) /bin/bash -c '\
		cd /go/src/github.com/alauda/kube-ovn && \
		make test && \
		make build-go '
