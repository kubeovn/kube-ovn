GOFILES_NOVENDOR=$(shell find . -type f -name '*.go' -not -path "./vendor/*")
GO_VERSION=1.13

REGISTRY=index.alauda.cn/alaudak8s
DEV_TAG=dev
RELEASE_TAG=$(shell cat VERSION)
OVS_TAG=200403

.PHONY: build-dev-images build-go build-bin lint up down halt suspend resume kind-init kind-init-ha kind-reload push-dev push-release e2e ut

build-dev-images: build-bin
	docker build -t ${REGISTRY}/kube-ovn:${DEV_TAG} -f dist/images/Dockerfile dist/images/

push-dev:
	docker push ${REGISTRY}/kube-ovn:${DEV_TAG}

build-go:
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn -ldflags "-w -s" -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-controller -ldflags "-w -s" -v ./cmd/controller
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-daemon -ldflags "-w -s" -v ./cmd/daemon
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-webhook -ldflags "-w -s" -v ./cmd/webhook
	CGO_ENABLED=0 GOOS=linux go build -o $(PWD)/dist/images/kube-ovn-pinger -ldflags "-w -s" -v ./cmd/pinger

ovs:
	docker build -t ovs:latest -f dist/ovs/Dockerfile dist/ovs/

release: lint build-go
	docker build -t ${REGISTRY}/kube-ovn:${RELEASE_TAG} -f dist/images/Dockerfile dist/images/

push-release:
	docker push ${REGISTRY}/kube-ovn:${RELEASE_TAG}

lint:
	@gofmt -d ${GOFILES_NOVENDOR} 
	@gofmt -l ${GOFILES_NOVENDOR} | read && echo "Code differs from gofmt's style" 1>&2 && exit 1 || true
	@GOOS=linux go vet ./...

build-bin:
	docker run --rm -e GOOS=linux -e GOCACHE=/tmp \
		-u $(shell id -u):$(shell id -g) \
		-v $(CURDIR):/go/src/github.com/alauda/kube-ovn:ro \
		-v $(CURDIR)/dist:/go/src/github.com/alauda/kube-ovn/dist/ \
		golang:$(GO_VERSION) /bin/bash -c '\
		cd /go/src/github.com/alauda/kube-ovn && \
		make build-go '

up:
	cd vagrant && vagrant up

down:
	cd vagrant && vagrant destroy -f

halt:
	cd vagrant && vagrant halt

resume:
	cd vagrant && vagrant resume

suspend:
	cd vagrant && vagrant suspend

kind-init:
	kind delete cluster --name=kube-ovn
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn:${RELEASE_TAG}
	kubectl label node kube-ovn-control-plane kube-ovn/role=master --overwrite
	kubectl apply -f yamls/crd.yaml
	kubectl apply -f yamls/ovn.yaml
	kubectl apply -f yamls/kube-ovn.yaml

kind-init-ha:
	kind delete cluster --name=kube-ovn
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn:${RELEASE_TAG}
	kind load docker-image --name kube-ovn nfvpe/multus:v3.4
	bash dist/images/install.sh

kind-reload:
	kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn:${RELEASE_TAG}
	kubectl delete pod -n kube-system -l app=kube-ovn-controller

kind-clean:
	kind delete cluster --name=kube-ovn

uninstall:
	bash dist/images/cleanup.sh

e2e:
	docker pull index.alauda.cn/claas/pause:3.1
	kind load docker-image --name kube-ovn index.alauda.cn/claas/pause:3.1
	ginkgo -p --slowSpecThreshold=60 test/e2e

ut:
	ginkgo -p --slowSpecThreshold=60 test/unittest
