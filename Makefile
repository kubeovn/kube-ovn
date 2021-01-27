GOFILES_NOVENDOR=$(shell find . -type f -name '*.go' -not -path "./vendor/*")
GO_VERSION=1.15

REGISTRY=kubeovn
DEV_TAG=dev
RELEASE_TAG=$(shell cat VERSION)
COMMIT=git-$(shell git rev-parse HEAD)
DATE=$(shell date +"%Y-%m-%d_%H:%M:%S")
GOLDFLAGS="-w -s -X github.com/alauda/kube-ovn/versions.COMMIT=${COMMIT} -X github.com/alauda/kube-ovn/versions.VERSION=${RELEASE_TAG} -X github.com/alauda/kube-ovn/versions.BUILDDATE=${DATE}"

# ARCH could be amd64,arm64
ARCH=amd64
# RPM_ARCH could be x86_64,aarch64
RPM_ARCH=x86_64

.PHONY: build-dev-images build-dpdk build-go build-bin lint kind-init kind-init-ha kind-install kind-install-ipv6 kind-reload push-dev push-release e2e ut

build-dev-images: build-bin
	docker build -t ${REGISTRY}/kube-ovn:${DEV_TAG} --build-arg ARCH=amd64 -f dist/images/Dockerfile dist/images/

build-dpdk:
	docker buildx build --cache-from "type=local,src=/tmp/.buildx-cache" --cache-to "type=local,dest=/tmp/.buildx-cache" --platform linux/amd64 -t ${REGISTRY}/kube-ovn-dpdk:19.11-${RELEASE_TAG} -o type=docker -f dist/images/Dockerfile.dpdk1911 dist/images/

push-dev:
	docker push ${REGISTRY}/kube-ovn:${DEV_TAG}

build-go:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(PWD)/dist/images/kube-ovn -ldflags $(GOLDFLAGS) -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(PWD)/dist/images/kube-ovn-cmd -ldflags $(GOLDFLAGS) -v ./cmd

build-go-arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(PWD)/dist/images/kube-ovn -ldflags $(GOLDFLAGS) -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o $(PWD)/dist/images/kube-ovn-cmd -ldflags $(GOLDFLAGS) -v ./cmd

release: lint build-go
	docker buildx build --cache-from "type=local,src=/tmp/.buildx-cache" --cache-to "type=local,dest=/tmp/.buildx-cache" --platform linux/amd64 --build-arg ARCH=amd64 --build-arg RPM_ARCH=x86_64 -t ${REGISTRY}/kube-ovn:${RELEASE_TAG} -o type=docker -f dist/images/Dockerfile dist/images/

release-arm: lint build-go-arm
	docker buildx build --cache-from "type=local,src=/tmp/.buildx-cache" --cache-to "type=local,dest=/tmp/.buildx-cache" --platform linux/arm64 --build-arg ARCH=arm64 --build-arg RPM_ARCH=aarch64 -t ${REGISTRY}/kube-ovn:${RELEASE_TAG} -o type=docker -f dist/images/Dockerfile dist/images/

tar:
	docker save ${REGISTRY}/kube-ovn:${RELEASE_TAG} > image.tar

push-release: release
	docker push ${REGISTRY}/kube-ovn:${RELEASE_TAG}

lint:
	@gofmt -d ${GOFILES_NOVENDOR}
	@gofmt -l ${GOFILES_NOVENDOR} | read && echo "Code differs from gofmt's style" 1>&2 && exit 1 || true
	@GOOS=linux go vet ./...
	@GOOS=linux gosec -exclude=G204 ./...

build-bin:
	docker run --rm -e GOOS=linux -e GOCACHE=/tmp -e GOARCH=${ARCH} -e GOPROXY=https://goproxy.cn \
		-u $(shell id -u):$(shell id -g) \
		-v $(CURDIR):/go/src/github.com/alauda/kube-ovn:ro \
		-v $(CURDIR)/dist:/go/src/github.com/alauda/kube-ovn/dist/ \
		golang:$(GO_VERSION) /bin/bash -c '\
		cd /go/src/github.com/alauda/kube-ovn && \
		make build-go '

kind-init:
	kind delete cluster --name=kube-ovn
	kube_proxy_mode=ipvs ip_family=ipv4 ha=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no
	docker exec kube-ovn-control-plane ip link add link eth0 mac1 type macvlan
	docker exec kube-ovn-worker ip link add link eth0 mac1 type macvlan

kind-init-iptables:
	kind delete cluster --name=kube-ovn
	kube_proxy_mode=iptables ip_family=ipv4 ha=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no
	docker exec kube-ovn-control-plane ip link add link eth0 mac1 type macvlan
	docker exec kube-ovn-worker ip link add link eth0 mac1 type macvlan

kind-install:
	kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn:${RELEASE_TAG}
	kubectl taint node kube-ovn-control-plane node-role.kubernetes.io/master:NoSchedule-
	ENABLE_SSL=true dist/images/install.sh
	kubectl describe no

kind-init-ha:
	kind delete cluster --name=kube-ovn
	kube_proxy_mode=ipvs ip_family=ipv4 ha=true j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

kind-init-ipv6:
	kind delete cluster --name=kube-ovn
	kube_proxy_mode=iptables ip_family=ipv6 ha=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

kind-install-ipv6:
	kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn:${RELEASE_TAG}
	kubectl taint node kube-ovn-control-plane node-role.kubernetes.io/master:NoSchedule-
	ENABLE_SSL=true IPv6=true dist/images/install.sh

kind-init-dual:
	kind delete cluster --name=kube-ovn
	kube_proxy_mode=iptables ip_family=DualStack ha=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no
	docker exec kube-ovn-control-plane ip link add link eth0 mac1 type macvlan
	docker exec kube-ovn-worker ip link add link eth0 mac1 type macvlan
	docker exec kube-ovn-worker sysctl -w net.ipv6.conf.all.disable_ipv6=0
	docker exec kube-ovn-control-plane sysctl -w net.ipv6.conf.all.disable_ipv6=0

kind-install-dual:
	kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn:${RELEASE_TAG}
	kubectl taint node kube-ovn-control-plane node-role.kubernetes.io/master:NoSchedule-
	ENABLE_SSL=true DualStack=true dist/images/install.sh
	kubectl describe no

kind-reload:
	kind load docker-image --name kube-ovn ${REGISTRY}/kube-ovn:${RELEASE_TAG}
	kubectl delete pod -n kube-system -l app=kube-ovn-controller
	kubectl delete pod -n kube-system -l app=kube-ovn-cni
	kubectl delete pod -n kube-system -l app=kube-ovn-pinger

kind-clean:
	kind delete cluster --name=kube-ovn

uninstall:
	bash dist/images/cleanup.sh

e2e:
	docker pull nginx:alpine
	kind load docker-image --name kube-ovn nginx:alpine
	ginkgo -p --slowSpecThreshold=60 test/e2e

ut:
	ginkgo -p --slowSpecThreshold=60 test/unittest

scan:
	trivy image --light --exit-code=1 --severity=HIGH --ignore-unfixed kubeovn/kube-ovn:${RELEASE_TAG}
