GO_VERSION = 1.18
SHELL=/bin/bash

REGISTRY = kubeovn
DEV_TAG = dev
RELEASE_TAG = $(shell cat VERSION)
COMMIT = git-$(shell git rev-parse --short HEAD)
DATE = $(shell date +"%Y-%m-%d_%H:%M:%S")
GOLDFLAGS = "-w -s -extldflags '-z now' -X github.com/kubeovn/kube-ovn/versions.COMMIT=$(COMMIT) -X github.com/kubeovn/kube-ovn/versions.VERSION=$(RELEASE_TAG) -X github.com/kubeovn/kube-ovn/versions.BUILDDATE=$(DATE)"

CONTROL_PLANE_TAINTS = node-role.kubernetes.io/master node-role.kubernetes.io/control-plane

MULTUS_IMAGE = ghcr.io/k8snetworkplumbingwg/multus-cni:stable
MULTUS_YAML = https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset.yml

CILIUM_VERSION = 1.11.6
CILIUM_IMAGE_REPO = quay.io/cilium/cilium

VPC_NAT_GW_IMG = $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG)

# ARCH could be amd64,arm64
ARCH = amd64

.PHONY: build-go
build-go:
	go mod tidy
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -ldflags $(GOLDFLAGS) -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-webhook -ldflags $(GOLDFLAGS) -v ./cmd/webhook

.PHONY: build-go-windows
build-go-windows:
	go mod tidy
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/windows/kube-ovn.exe -ldflags $(GOLDFLAGS) -v ./cmd/windows/cni
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/windows/kube-ovn-daemon.exe -ldflags $(GOLDFLAGS) -v ./cmd/windows/daemon

.PHONY: build-go-arm
build-go-arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -ldflags $(GOLDFLAGS) -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-webhook -ldflags $(GOLDFLAGS) -v ./cmd/webhook

.PHONY: build-bin
build-bin:
	docker run --rm -e GOOS=linux -e GOCACHE=/tmp -e GOARCH=$(ARCH) -e GOPROXY=https://goproxy.cn \
		-u $(shell id -u):$(shell id -g) \
		-v $(CURDIR):/go/src/github.com/kubeovn/kube-ovn:ro \
		-v $(CURDIR)/dist:/go/src/github.com/kubeovn/kube-ovn/dist/ \
		golang:$(GO_VERSION) /bin/bash -c '\
		cd /go/src/github.com/kubeovn/kube-ovn && \
		make build-go '

.PHONY: build-dev-images
build-dev-images: build-bin
	docker build -t $(REGISTRY)/kube-ovn:$(DEV_TAG) --build-arg ARCH=amd64 -f dist/images/Dockerfile dist/images/

.PHONY: build-dpdk
build-dpdk:
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn-dpdk:19.11-$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile.dpdk1911 dist/images/

.PHONY: base-amd64
base-amd64:
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 --build-arg NO_AVX512=true -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-no-avx512 -o type=docker -f dist/images/Dockerfile.base dist/images/

.PHONY: base-amd64-dpdk
base-amd64-dpdk:
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o type=docker -f dist/images/Dockerfile.base-dpdk dist/images/

.PHONY: base-arm64
base-arm64:
	docker buildx build --platform linux/arm64 --build-arg ARCH=arm64 -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 -o type=docker -f dist/images/Dockerfile.base dist/images/

.PHONY: release
release: lint build-go
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-no-avx512 -o type=docker -f dist/images/Dockerfile.no-avx512 dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk -o type=docker -f dist/images/Dockerfile.dpdk dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/centos7-compile:$(RELEASE_TAG) -o type=docker -f dist/images/compile/centos7/Dockerfile fastpath/
#	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/centos8-compile:$(RELEASE_TAG) -o type=docker -f dist/images/compile/centos8/Dockerfile fastpath/

.PHONY: release-arm
release-arm: build-go-arm
	docker buildx build --platform linux/arm64 --build-arg ARCH=arm64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build --platform linux/arm64 --build-arg ARCH=arm64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHONY: push-dev
push-dev:
	docker push $(REGISTRY)/kube-ovn:$(DEV_TAG)

.PHONY: push-release
push-release: release
	docker push $(REGISTRY)/kube-ovn:$(RELEASE_TAG)

.PHONY: tar
tar:
	docker save $(REGISTRY)/kube-ovn:$(RELEASE_TAG) $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-no-avx512 -o kube-ovn.tar
	docker save $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o vpc-nat-gateway.tar
	docker save $(REGISTRY)/centos7-compile:$(RELEASE_TAG) -o centos7-compile.tar
#	docker save $(REGISTRY)/centos8-compile:$(RELEASE_TAG) -o centos8-compile.tar

.PHONY: base-tar-amd64
base-tar-amd64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-no-avx512 -o image-amd64.tar

.PHONY: base-tar-amd64-dpdk
base-tar-amd64-dpdk:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o image-amd64-dpdk.tar

.PHONY: base-tar-arm64
base-tar-arm64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 -o image-arm64.tar

.PHONY: kind-init
kind-init: kind-clean
	kube_proxy_mode=ipvs ip_family=ipv4 ha=false single=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

.PHONY: kind-init-cluster
kind-init-cluster: kind-clean-cluster
	kube_proxy_mode=ipvs ip_family=ipv4 ha=false single=true j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kind create cluster --config yamls/kind.yaml --name kube-ovn1
	kubectl config use-context kind-kube-ovn
	kubectl get no
	kubectl config use-context kind-kube-ovn1
	kubectl get no

.PHONY: kind-init-iptables
kind-init-iptables: kind-clean
	kube_proxy_mode=iptables ip_family=ipv4 ha=false single=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

.PHONY: kind-init-ha
kind-init-ha: kind-clean
	kube_proxy_mode=ipvs ip_family=ipv4 ha=true single=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

.PHONY: kind-init-single
kind-init-single: kind-clean
	kube_proxy_mode=ipvs ip_family=ipv4 ha=false single=true j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

.PHONY: kind-init-ipv6
kind-init-ipv6: kind-clean
	kube_proxy_mode=ipvs ip_family=ipv6 ha=false single=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

.PHONY: kind-init-dual
kind-init-dual: kind-clean
	kube_proxy_mode=ipvs ip_family=dual ha=false single=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no
	docker exec kube-ovn-worker sysctl -w net.ipv6.conf.all.disable_ipv6=0
	docker exec kube-ovn-control-plane sysctl -w net.ipv6.conf.all.disable_ipv6=0

.PHONY: kind-init-cilium
kind-init-cilium: kind-clean
	kind delete cluster --name=kube-ovn
	kube_proxy_mode=iptables ip_family=ipv4 ha=false single=false j2 yamls/kind.yaml.j2 -o yamls/kind.yaml
	kind create cluster --config yamls/kind.yaml --name kube-ovn
	kubectl describe no

.PHONY: kind-load-image
kind-load-image:
	kind load docker-image --name kube-ovn $(REGISTRY)/kube-ovn:$(RELEASE_TAG)

.PHONY: kind-untaint-control-plane
kind-untaint-control-plane:
	@for node in $$(kubectl get no -o jsonpath='{.items[*].metadata.name}'); do \
		for key in $(CONTROL_PLANE_TAINTS); do \
			taint=$$(kubectl get no $$node -o jsonpath="{.spec.taints[?(@.key==\"$$key\")]}"); \
			if [ -n "$$taint" ]; then \
				kubectl taint node $$node $$key:NoSchedule-; \
			fi; \
		done; \
	done

.PHONY: kind-install
kind-install: kind-load-image kind-untaint-control-plane
	ENABLE_SSL=true dist/images/install.sh
	kubectl describe no

.PHONY: kind-install-cluster
kind-install-cluster: kind-load-image
	kind load docker-image --name kube-ovn1 $(REGISTRY)/kube-ovn:$(RELEASE_TAG)
	kubectl config use-context kind-kube-ovn
	ENABLE_SSL=true dist/images/install.sh
	kubectl describe no
	kubectl config use-context kind-kube-ovn1
	sed -e 's/10.16.0/10.18.0/g' \
		-e 's/10.96.0/10.98.0/g' \
		-e 's/100.64.0/100.68.0/g' \
		dist/images/install.sh > install-multi.sh
	ENABLE_SSL=true bash install-multi.sh
	kubectl describe no

.PHONY: kind-install-underlay
kind-install-underlay: kind-load-image kind-untaint-control-plane
	$(eval SUBNET = $(shell docker network inspect kind -f "{{(index .IPAM.Config 0).Subnet}}"))
	$(eval GATEWAY = $(shell docker network inspect kind -f "{{(index .IPAM.Config 0).Gateway}}"))
	$(eval EXCLUDE_IPS = $(shell docker network inspect kind -f '{{range .Containers}},{{index (split .IPv4Address "/") 0}}{{end}}' | sed 's/^,//'))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh > install-underlay.sh
	ENABLE_SSL=true ENABLE_VLAN=true VLAN_NIC=eth0 bash install-underlay.sh
	kubectl describe no

.PHONY: kind-install-single
kind-install-single: kind-load-image
	ENABLE_SSL=true dist/images/install.sh
	kubectl describe no

.PHONY: kind-install-ipv6
kind-install-ipv6: kind-load-image kind-untaint-control-plane
	ENABLE_SSL=true IPV6=true dist/images/install.sh

.PHONY: kind-install-underlay-ipv6
kind-install-underlay-ipv6: kind-load-image kind-untaint-control-plane
	$(eval SUBNET = $(shell docker network inspect kind -f "{{(index .IPAM.Config 1).Subnet}}"))
	$(eval GATEWAY = $(shell docker exec kube-ovn-control-plane ip -6 route show default | awk '{print $$3}'))
	$(eval EXCLUDE_IPS = $(shell docker network inspect kind -f '{{range .Containers}},{{index (split .IPv6Address "/") 0}}{{end}}' | sed 's/^,//'))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh > install-underlay.sh
	ENABLE_SSL=true IPV6=true ENABLE_VLAN=true VLAN_NIC=eth0 bash install-underlay.sh

.PHONY: kind-install-dual
kind-install-dual: kind-load-image kind-untaint-control-plane
	ENABLE_SSL=true DUAL_STACK=true dist/images/install.sh
	kubectl describe no

.PHONY: kind-install-underlay-dual
kind-install-underlay-dual: kind-load-image kind-untaint-control-plane
	$(eval IPV4_SUBNET = $(shell docker network inspect kind -f "{{(index .IPAM.Config 0).Subnet}}"))
	$(eval IPV6_SUBNET = $(shell docker network inspect kind -f "{{(index .IPAM.Config 1).Subnet}}"))
	$(eval IPV4_GATEWAY = $(shell docker network inspect kind -f "{{(index .IPAM.Config 0).Gateway}}"))
	$(eval IPV6_GATEWAY = $(shell docker exec kube-ovn-control-plane ip -6 route show default | awk '{print $$3}'))
	$(eval IPV4_EXCLUDE_IPS = $(shell docker network inspect kind -f '{{range .Containers}},{{index (split .IPv4Address "/") 0}}{{end}}' | sed 's/^,//'))
	$(eval IPV6_EXCLUDE_IPS = $(shell docker network inspect kind -f '{{range .Containers}},{{index (split .IPv6Address "/") 0}}{{end}}' | sed 's/^,//'))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(IPV4_SUBNET),$(IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(IPV4_GATEWAY),$(IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(IPV4_EXCLUDE_IPS),$(IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh > install-underlay.sh
	ENABLE_SSL=true DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=eth0 bash install-underlay.sh

.PHONY: kind-install-underlay-logical-gateway-dual
kind-install-underlay-logical-gateway-dual: kind-load-image kind-untaint-control-plane
	$(eval IPV4_SUBNET = $(shell docker network inspect kind -f "{{(index .IPAM.Config 0).Subnet}}"))
	$(eval IPV6_SUBNET = $(shell docker network inspect kind -f "{{(index .IPAM.Config 1).Subnet}}"))
	$(eval IPV4_GATEWAY = $(shell docker network inspect kind -f "{{(index .IPAM.Config 0).Gateway}}"))
	$(eval IPV6_GATEWAY = $(shell docker exec kube-ovn-control-plane ip -6 route show default | awk '{print $$3}'))
	$(eval IPV4_EXCLUDE_IPS = $(shell docker network inspect kind -f '{{range .Containers}},{{index (split .IPv4Address "/") 0}}{{end}}' | sed 's/^,//'))
	$(eval IPV6_EXCLUDE_IPS = $(shell docker network inspect kind -f '{{range .Containers}},{{index (split .IPv6Address "/") 0}}{{end}}' | sed 's/^,//'))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(IPV4_SUBNET),$(IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(IPV4_GATEWAY)9,$(IPV6_GATEWAY)f"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(IPV4_EXCLUDE_IPS),$(IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh > install-underlay.sh
	ENABLE_SSL=true DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=eth0 LOGICAL_GATEWAY=true bash install-underlay.sh

.PHONY: kind-install-multus
kind-install-multus: kind-load-image kind-untaint-control-plane
	if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep -qw "^$(MULTUS_IMAGE)$$"; then \
		docker pull "$(MULTUS_IMAGE)"; \
	fi

	kind load docker-image --name kube-ovn "$(MULTUS_IMAGE)"
	kubectl apply -f "$(MULTUS_YAML)"
	kubectl -n kube-system rollout status ds kube-multus-ds
	kubectl apply -f yamls/lb-svc-attachment.yaml
	kind load docker-image --name kube-ovn $(REGISTRY)/kube-ovn:$(RELEASE_TAG)
	kind load docker-image --name kube-ovn $(VPC_NAT_GW_IMG)
	ENABLE_SSL=true ENABLE_LB_SVC=true CNI_CONFIG_PRIORITY=10 dist/images/install.sh
	kubectl describe no

.PHONY: kind-install-ic
kind-install-ic:
	docker run -d --name ovn-ic-db --network kind $(REGISTRY)/kube-ovn:$(RELEASE_TAG) bash start-ic-db.sh
	@set -e; \
	ic_db_host=$$(docker inspect ovn-ic-db -f "{{.NetworkSettings.Networks.kind.IPAddress}}"); \
	zone=az0 ic_db_host=$$ic_db_host gateway_node_name=kube-ovn-control-plane j2 yamls/ovn-ic.yaml.j2 -o ovn-ic-0.yaml; \
	zone=az1 ic_db_host=$$ic_db_host gateway_node_name=kube-ovn1-control-plane j2 yamls/ovn-ic.yaml.j2 -o ovn-ic-1.yaml; \
	zone=az1111 ic_db_host=$$ic_db_host gateway_node_name=kube-ovn1-control-plane j2 yamls/ovn-ic.yaml.j2 -o /tmp/ovn-ic-1-alter.yaml
	kubectl config use-context kind-kube-ovn
	kubectl apply -f ovn-ic-0.yaml
	sleep 6
	kubectl -n kube-system get pods | grep ovs-ovn | awk '{print $$1}' | xargs kubectl -n kube-system delete pod
	kubectl config use-context kind-kube-ovn1
	kubectl apply -f ovn-ic-1.yaml
	sleep 6
	kubectl -n kube-system get pods | grep ovs-ovn | awk '{print $$1}' | xargs kubectl -n kube-system delete pod


.PHONY: kind-install-cilium
kind-install-cilium: kind-load-image kind-untaint-control-plane
	$(eval KUBERNETES_SERVICE_HOST = $(shell kubectl get nodes kube-ovn-control-plane -o jsonpath='{.status.addresses[0].address}'))
	if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep -qw "^$(CILIUM_IMAGE_REPO):v$(CILIUM_VERSION)$$"; then \
		docker pull "$(CILIUM_IMAGE_REPO):v$(CILIUM_VERSION)"; \
	fi
	kind load docker-image --name kube-ovn "$(CILIUM_IMAGE_REPO):v$(CILIUM_VERSION)"
	kubectl apply -f yamls/chaining.yaml
	helm repo add cilium https://helm.cilium.io/
	helm install cilium cilium/cilium \
		--version $(CILIUM_VERSION) \
		--namespace=kube-system \
		--set k8sServiceHost=$(KUBERNETES_SERVICE_HOST) \
		--set k8sServicePort=6443 \
		--set tunnel=disabled \
		--set enableIPv4Masquerade=false \
		--set enableIdentityMark=false \
		--set cni.chainingMode=generic-veth \
		--set cni.customConf=true \
		--set cni.configMap=cni-configuration
	kubectl -n kube-system rollout status ds cilium --timeout 300s
	bash dist/images/cilium.sh
	ENABLE_SSL=true ENABLE_LB=false ENABLE_NP=false WITHOUT_KUBE_PROXY=true CNI_CONFIG_PRIORITY=10 bash dist/images/install.sh
	kubectl describe no

.PHONY: kind-reload
kind-reload: kind-load-image
	kubectl delete pod -n kube-system -l app=kube-ovn-controller
	kubectl delete pod -n kube-system -l app=kube-ovn-cni
	kubectl delete pod -n kube-system -l app=kube-ovn-pinger
	kubectl delete pod -n kube-system -l app=ovs

.PHONY: kind-reload-ovs
kind-reload-ovs: kind-load-image
	kubectl delete pod -n kube-system -l app=ovs

.PHONY: kind-clean
kind-clean:
	kind delete cluster --name=kube-ovn
	docker ps -a -f name=kube-ovn-e2e --format "{{.ID}}" | while read c; do docker rm -f $$c; done

.PHONY: kind-clean-cluster
kind-clean-cluster:
	kind delete cluster --name=kube-ovn
	kind delete cluster --name=kube-ovn1
	docker ps -a -f name=ovn-ic-db --format "{{.ID}}" | while read c; do docker rm -f $$c; done

.PHONY: uninstall
uninstall:
	bash dist/images/cleanup.sh

.PHONY: lint
lint:
	@gofmt -d .
	@if [ $$(gofmt -l . | wc -l) -ne 0 ]; then \
		echo "Code differs from gofmt's style" 1>&2 && exit 1; \
	fi
	@GOOS=linux go vet ./...
	@GOOS=linux gosec -exclude=G204,G306,G404,G601,G301 -exclude-dir=test -exclude-dir=pkg/client ./...

.PHONY: lint-windows
lint-windows:
	@GOOS=windows go vet ./cmd/windows/...
	@GOOS=windows gosec -exclude=G204,G601,G301 ./pkg/util
	@GOOS=windows gosec -exclude=G204,G601,G301 ./pkg/request
	@GOOS=windows gosec -exclude=G204,G601,G301 ./cmd/cni

.PHONY: scan
scan:
	trivy image --exit-code=1 --severity=HIGH --ignore-unfixed --security-checks vuln $(REGISTRY)/kube-ovn:$(RELEASE_TAG)
	trivy image --exit-code=1 --severity=HIGH --ignore-unfixed --security-checks vuln $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG)

.PHONY: ut
ut:
	ginkgo -mod=mod -progress -reportPassed --slowSpecThreshold=60 test/unittest
	go test ./pkg/...

.PHONY: e2e
e2e:
	$(eval NODE_COUNT = $(shell kind get nodes --name kube-ovn | wc -l))
	$(eval NETWORK_BRIDGE = $(shell docker inspect -f '{{json .NetworkSettings.Networks.bridge}}' kube-ovn-control-plane))
	@if docker ps -a --format 'table {{.Names}}' | grep -q '^kube-ovn-e2e$$'; then \
		docker rm -f kube-ovn-e2e; \
	fi
	docker run -d --name kube-ovn-e2e --network kind --cap-add=NET_ADMIN $(REGISTRY)/kube-ovn:$(RELEASE_TAG) sleep infinity
	@if [ '$(NETWORK_BRIDGE)' = 'null' ]; then \
		kind get nodes --name kube-ovn | while read node; do \
		docker network connect bridge $$node; \
		done; \
	fi

	@if [ -n "$$VLAN_ID" ]; then \
		kind get nodes --name kube-ovn | while read node; do \
			docker cp test/kind-vlan.sh $$node:/kind-vlan.sh; \
			docker exec $$node sh -c "VLAN_ID=$$VLAN_ID sh /kind-vlan.sh"; \
		done; \
	fi

	@echo "{" > test/e2e/network.json
	@i=0; kind get nodes --name kube-ovn | while read node; do \
		i=$$((i+1)); \
		printf '"%s": ' "$$node" >> test/e2e/network.json; \
		docker inspect -f "{{json .NetworkSettings.Networks.bridge}}" "$$node" >> test/e2e/network.json; \
		if [ $$i -ne $(NODE_COUNT) ]; then echo "," >> test/e2e/network.json; fi; \
	done
	@echo "}" >> test/e2e/network.json

	@if [ ! -n "$$(docker images -q kubeovn/pause:3.2 2>/dev/null)" ]; then docker pull kubeovn/pause:3.2; fi
	kind load docker-image --name kube-ovn kubeovn/pause:3.2
	ginkgo -mod=mod -progress -reportPassed --slowSpecThreshold=60 test/e2e

.PHONY: e2e-ipv6
e2e-ipv6:
	@IPV6=true $(MAKE) e2e

.PHONY: e2e-vlan
e2e-vlan:
	@VLAN_ID=100 $(MAKE) e2e

.PHONY: e2e-vlan-ipv6
e2e-vlan-ipv6:
	@IPV6=true $(MAKE) e2e-vlan

.PHONY: e2e-underlay-single-nic
e2e-underlay-single-nic:
	@docker inspect -f '{{json .NetworkSettings.Networks.kind}}' kube-ovn-control-plane > test/e2e-underlay-single-nic/node/network.json
	ginkgo -mod=mod -progress -reportPassed --slowSpecThreshold=60 test/e2e-underlay-single-nic

.PHONY: e2e-ovn-ic
e2e-ovn-ic:
	ginkgo -mod=mod -progress -reportPassed --slowSpecThreshold=60 test/e2e-ovnic

.PHONY: e2e-ovn-ebpf
e2e-ovn-ebpf:
	docker run -d --name kube-ovn-e2e --network kind --cap-add=NET_ADMIN $(REGISTRY)/kube-ovn:$(RELEASE_TAG) sleep infinity
	ginkgo -mod=mod -progress -reportPassed --slowSpecThreshold=60 test/e2e-ebpf

.PHONY: e2e-multus
e2e-multus:
	ginkgo -mod=mod -progress -reportPassed --slowSpecThreshold=60 test/e2e-multus

.PHONY: clean
clean:
	$(RM) dist/images/kube-ovn dist/images/kube-ovn-cmd
	$(RM) yamls/kind.yaml
	$(RM) ovn.yaml kube-ovn.yaml kube-ovn-crd.yaml
	$(RM) ovn-ic-0.yaml ovn-ic-1.yaml
	$(RM) kube-ovn.tar vpc-nat-gateway.tar image-amd64.tar image-arm64.tar
	$(RM) test/e2e/ovnnb_db.* test/e2e/ovnsb_db.*
	$(RM) install-underlay.sh
