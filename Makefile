SHELL = /bin/bash

include kind.mk
include talos.mk
include e2e.mk

REGISTRY = kubeovn
DEV_TAG = dev
RELEASE_TAG = $(shell cat VERSION)
DEBUG_TAG = $(shell cat VERSION)-debug
LEGACY_TAG = $(shell cat VERSION)-amd64-legacy
VERSION = $(shell echo $${VERSION:-$(RELEASE_TAG)})
COMMIT = git-$(shell git rev-parse --short HEAD)
DATE = $(shell date +"%Y-%m-%d_%H:%M:%S")

GOLDFLAGS = -extldflags '-z now' -X github.com/kubeovn/kube-ovn/versions.COMMIT=$(COMMIT) -X github.com/kubeovn/kube-ovn/versions.VERSION=$(RELEASE_TAG) -X github.com/kubeovn/kube-ovn/versions.BUILDDATE=$(DATE)
ifdef DEBUG
GO_BUILD_FLAGS = -ldflags "$(GOLDFLAGS)"
else
GO_BUILD_FLAGS = -trimpath -ldflags "-w -s $(GOLDFLAGS)"
endif

OS_LINUX = 0
ifneq ($(OS),Windows_NT)
ifeq ($(shell uname -s),Linux)
OS_LINUX = 1
endif
endif

CONTROL_PLANE_TAINTS = node-role.kubernetes.io/master node-role.kubernetes.io/control-plane

FRR_VERSION = 9.1.3
FRR_IMAGE = quay.io/frrouting/frr:$(FRR_VERSION)

CLAB_IMAGE = ghcr.io/srl-labs/clab:0.66.0

MULTUS_VERSION = v4.2.0
MULTUS_IMAGE = ghcr.io/k8snetworkplumbingwg/multus-cni:$(MULTUS_VERSION)-thick
MULTUS_YAML = https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/$(MULTUS_VERSION)/deployments/multus-daemonset-thick.yml

METALLB_VERSION = 0.14.9
METALLB_CHART_REPO = https://metallb.github.io/metallb
METALLB_CONTROLLER_IMAGE = quay.io/metallb/controller:v$(METALLB_VERSION)
METALLB_SPEAKER_IMAGE = quay.io/metallb/speaker:v$(METALLB_VERSION)

KUBEVIRT_VERSION = v1.5.0
KUBEVIRT_OPERATOR_IMAGE = quay.io/kubevirt/virt-operator:$(KUBEVIRT_VERSION)
KUBEVIRT_API_IMAGE = quay.io/kubevirt/virt-api:$(KUBEVIRT_VERSION)
KUBEVIRT_CONTROLLER_IMAGE = quay.io/kubevirt/virt-controller:$(KUBEVIRT_VERSION)
KUBEVIRT_HANDLER_IMAGE = quay.io/kubevirt/virt-handler:$(KUBEVIRT_VERSION)
KUBEVIRT_LAUNCHER_IMAGE = quay.io/kubevirt/virt-launcher:$(KUBEVIRT_VERSION)
KUBEVIRT_OPERATOR_YAML = https://github.com/kubevirt/kubevirt/releases/download/$(KUBEVIRT_VERSION)/kubevirt-operator.yaml
KUBEVIRT_CR_YAML = https://github.com/kubevirt/kubevirt/releases/download/$(KUBEVIRT_VERSION)/kubevirt-cr.yaml

CILIUM_VERSION = 1.17.2
CILIUM_IMAGE_REPO = quay.io/cilium

CERT_MANAGER_VERSION = v1.17.1
CERT_MANAGER_CONTROLLER = quay.io/jetstack/cert-manager-controller:$(CERT_MANAGER_VERSION)
CERT_MANAGER_CAINJECTOR = quay.io/jetstack/cert-manager-cainjector:$(CERT_MANAGER_VERSION)
CERT_MANAGER_WEBHOOK = quay.io/jetstack/cert-manager-webhook:$(CERT_MANAGER_VERSION)
CERT_MANAGER_YAML = https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml

SUBMARINER_VERSION = $(shell echo $${SUBMARINER_VERSION:-0.19.3})
SUBMARINER_OPERATOR = quay.io/submariner/submariner-operator:$(SUBMARINER_VERSION)
SUBMARINER_GATEWAY = quay.io/submariner/submariner-gateway:$(SUBMARINER_VERSION)
SUBMARINER_LIGHTHOUSE_AGENT = quay.io/submariner/lighthouse-agent:$(SUBMARINER_VERSION)
SUBMARINER_LIGHTHOUSE_COREDNS = quay.io/submariner/lighthouse-coredns:$(SUBMARINER_VERSION)
SUBMARINER_ROUTE_AGENT = quay.io/submariner/submariner-route-agent:$(SUBMARINER_VERSION)
SUBMARINER_NETTEST = quay.io/submariner/nettest:$(SUBMARINER_VERSION)

DEEPFLOW_VERSION = v6.4
DEEPFLOW_CHART_VERSION = 6.4.013
DEEPFLOW_CHART_REPO = https://deepflow-ce.oss-cn-beijing.aliyuncs.com/chart/stable
DEEPFLOW_IMAGE_REPO = registry.cn-beijing.aliyuncs.com/deepflow-ce
DEEPFLOW_SERVER_NODE_PORT = 30417
DEEPFLOW_SERVER_GRPC_PORT = 30035
DEEPFLOW_SERVER_HTTP_PORT = 20417
DEEPFLOW_GRAFANA_NODE_PORT = 30080
DEEPFLOW_MAPPED_PORTS = $(DEEPFLOW_SERVER_NODE_PORT),$(DEEPFLOW_SERVER_GRPC_PORT),$(DEEPFLOW_SERVER_HTTP_PORT),$(DEEPFLOW_GRAFANA_NODE_PORT)
DEEPFLOW_CTL_URL = https://deepflow-ce.oss-cn-beijing.aliyuncs.com/bin/ctl/$(DEEPFLOW_VERSION)/linux/$(shell arch | sed 's|x86_64|amd64|' | sed 's|aarch64|arm64|')/deepflow-ctl

KWOK_VERSION = v0.6.1
KWOK_IMAGE = registry.k8s.io/kwok/kwok:$(KWOK_VERSION)

VPC_NAT_GW_IMG = $(REGISTRY)/vpc-nat-gateway:$(VERSION)

ANP_TEST_IMAGE = registry.k8s.io/e2e-test-images/agnhost:2.45
ANP_CR_YAML = https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/refs/heads/main/config/crd/experimental/policy.networking.k8s.io_adminnetworkpolicies.yaml
BANP_CR_YAML = https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/refs/heads/main/config/crd/experimental/policy.networking.k8s.io_baselineadminnetworkpolicies.yaml

E2E_NETWORK = kube-ovn-vlan

KIND_NETWORK_UNDERLAY = $(shell echo $${KIND_NETWORK_UNDERLAY:-kind})
UNDERLAY_VAR_PREFIX = $(shell echo $(KIND_NETWORK_UNDERLAY) | tr '[:lower:]-' '[:upper:]_')
UNDERLAY_IPV4_SUBNET = $(UNDERLAY_VAR_PREFIX)_IPV4_SUBNET
UNDERLAY_IPV6_SUBNET = $(UNDERLAY_VAR_PREFIX)_IPV6_SUBNET
UNDERLAY_IPV4_GATEWAY = $(UNDERLAY_VAR_PREFIX)_IPV4_GATEWAY
UNDERLAY_IPV6_GATEWAY = $(UNDERLAY_VAR_PREFIX)_IPV6_GATEWAY
UNDERLAY_IPV4_EXCLUDE_IPS = $(UNDERLAY_VAR_PREFIX)_IPV4_EXCLUDE_IPS
UNDERLAY_IPV6_EXCLUDE_IPS = $(UNDERLAY_VAR_PREFIX)_IPV6_EXCLUDE_IPS

VLAN_NIC = $(shell echo $${VLAN_NIC:-eth0})
ifneq ($(KIND_NETWORK_UNDERLAY),kind)
VLAN_NIC = eth1
endif

KIND_AUDITING = $(shell echo $${KIND_AUDITING:-false})
ifeq ($(shell echo $${CI:-false}),true)
KIND_AUDITING = true
endif

# ARCH could be amd64,arm64
ARCH = amd64

.PHONY: build-go
build-go:
	go mod tidy
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/images/kube-ovn -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-daemon -v ./cmd/daemon
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-controller -v ./cmd/controller
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/images/test-server -v ./test/server

.PHONY: build-go-windows
build-go-windows:
	go mod tidy
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/windows/kube-ovn.exe -v ./cmd/cni
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/windows/kube-ovn-daemon.exe -v ./cmd/daemon

.PHONY: build-go-arm
build-go-arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -o $(CURDIR)/dist/images/kube-ovn -v ./cmd/cni
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-daemon -v ./cmd/daemon
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-controller -v ./cmd/controller

.PHONY: build-kube-ovn
build-kube-ovn: build-debug build-go
	docker build -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -f dist/images/Dockerfile dist/images/
	docker build -t $(REGISTRY)/kube-ovn:$(LEGACY_TAG) --build-arg VERSION=$(LEGACY_TAG) -f dist/images/Dockerfile dist/images/

.PHONY: build-kube-ovn-dpdk
build-kube-ovn-dpdk: build-go
	docker build -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk --build-arg BASE_TAG=$(RELEASE_TAG)-dpdk -f dist/images/Dockerfile dist/images/

.PHONY: build-dev
build-dev: build-go
	docker build -t $(REGISTRY)/kube-ovn:$(DEV_TAG) --build-arg VERSION=$(RELEASE_TAG) -f dist/images/Dockerfile dist/images/

.PHONY: build-debug
build-debug:
	@DEBUG=1 $(MAKE) build-go
	docker build -t $(REGISTRY)/kube-ovn:$(DEBUG_TAG) --build-arg BASE_TAG=$(DEBUG_TAG) -f dist/images/Dockerfile dist/images/

.PHONY: base-amd64
base-amd64:
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY --build-arg LEGACY=true -t $(REGISTRY)/kube-ovn-base:$(LEGACY_TAG) -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY --build-arg DEBUG=true -t $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-amd64 -o type=docker -f dist/images/Dockerfile.base dist/images/

.PHONY: base-amd64-dpdk
base-amd64-dpdk:
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o type=docker -f dist/images/Dockerfile.base-dpdk dist/images/

.PHONY: base-arm64
base-arm64:
	docker buildx build --platform linux/arm64 --build-arg ARCH=arm64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build --platform linux/arm64 --build-arg ARCH=arm64 --build-arg GO_VERSION --build-arg TRIVY_DB_REPOSITORY --build-arg DEBUG=true -t $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-arm64 -o type=docker -f dist/images/Dockerfile.base dist/images/


.PHONY: build-kit
build-kit: build-go
	DOCKER_BUILDKIT=1 docker build -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn
image-kube-ovn: image-kube-ovn-debug build-go
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(LEGACY_TAG) --build-arg VERSION=$(LEGACY_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn-arm64
image-kube-ovn-arm64: build-go-arm
	docker buildx build --platform linux/arm64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn-debug
image-kube-ovn-debug:
	@DEBUG=1 $(MAKE) build-go
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(DEBUG_TAG) --build-arg BASE_TAG=$(DEBUG_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-kube-ovn-dpdk
image-kube-ovn-dpdk: build-go
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk --build-arg VERSION=$(RELEASE_TAG) --build-arg BASE_TAG=$(RELEASE_TAG)-dpdk -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: image-vpc-nat-gateway
image-vpc-nat-gateway:
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHOONY: image-test
image-test: build-go
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/test:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile.test dist/images/

.PHONY: release
release: lint image-kube-ovn image-vpc-nat-gateway

.PHONY: release-arm
release-arm: release-arm-debug image-kube-ovn-arm64
	docker buildx build --platform linux/arm64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHONY: release-arm-debug
release-arm-debug:
	@DEBUG=1 $(MAKE) build-go-arm
	docker buildx build --platform linux/arm64 -t $(REGISTRY)/kube-ovn:$(DEBUG_TAG) --build-arg BASE_TAG=$(DEBUG_TAG) -o type=docker -f dist/images/Dockerfile dist/images/

.PHONY: push-dev
push-dev:
	docker push $(REGISTRY)/kube-ovn:$(DEV_TAG)

.PHONY: push-release
push-release: release
	docker push $(REGISTRY)/kube-ovn:$(RELEASE_TAG)

.PHONY: tar-kube-ovn
tar-kube-ovn:
	docker save $(REGISTRY)/kube-ovn:$(RELEASE_TAG) $(REGISTRY)/kube-ovn:$(LEGACY_TAG) $(REGISTRY)/kube-ovn:$(DEBUG_TAG) -o kube-ovn.tar

.PHONY: tar-kube-ovn-dpdk
tar-kube-ovn-dpdk:
	docker save $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk -o kube-ovn-dpdk.tar

.PHONY: tar-vpc-nat-gateway
tar-vpc-nat-gateway:
	docker save $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o vpc-nat-gateway.tar

.PHONY: tar
tar: tar-kube-ovn tar-vpc-nat-gateway

.PHONY: base-tar-amd64
base-tar-amd64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 $(REGISTRY)/kube-ovn-base:$(LEGACY_TAG) $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-amd64 -o image-amd64.tar

.PHONY: base-tar-amd64-dpdk
base-tar-amd64-dpdk:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o image-amd64-dpdk.tar

.PHONY: base-tar-arm64
base-tar-arm64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-arm64 -o image-arm64.tar

.PHONY: untaint-control-plane
untaint-control-plane:
	@for node in $(shell kubectl get node -o jsonpath='{.items[*].metadata.name}'); do \
		for key in $(CONTROL_PLANE_TAINTS); do \
			taint=$$(kubectl get no $$node -o jsonpath="{.spec.taints[?(@.key==\"$$key\")]}"); \
			if [ -n "$$taint" ]; then \
				kubectl taint node $$node $$key:NoSchedule-; \
			fi; \
		done; \
	done

define docker_ensure_image_exists
	if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep "^$(1)$$" >/dev/null; then \
		docker pull "$(1)"; \
	fi
endef

define docker_rm_container
	@docker ps -a -q -f name="^$(1)$$" | while read c; do docker rm -f $$c; done
endef

define docker_network_info
	$(eval VAR_PREFIX = $(shell echo $(1) | tr '[:lower:]-' '[:upper:]_'))
	$(eval $(VAR_PREFIX)_IPV4_SUBNET = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Subnet}}{{end}}" | grep -v :))
	$(eval $(VAR_PREFIX)_IPV6_SUBNET = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Subnet}}{{end}}" | grep :))
	$(eval $(VAR_PREFIX)_IPV4_GATEWAY = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Gateway}}{{end}}" | grep -v :))
	$(eval $(VAR_PREFIX)_IPV6_GATEWAY = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Gateway}}{{end}}" | grep :))
	$(eval $(VAR_PREFIX)_IPV6_GATEWAY := $(if $($(VAR_PREFIX)_IPV6_GATEWAY),$($(VAR_PREFIX)_IPV6_GATEWAY),$(shell docker exec kube-ovn-control-plane ip -6 route show default | awk '{print $$3}')))
	$(eval $(VAR_PREFIX)_IPV4_EXCLUDE_IPS = $(shell docker network inspect $(1) -f '{{range .Containers}},{{index (split .IPv4Address "/") 0}}{{end}}' | sed 's/^,//'))
	$(eval $(VAR_PREFIX)_IPV6_EXCLUDE_IPS = $(shell docker network inspect $(1) -f '{{range .Containers}},{{index (split .IPv6Address "/") 0}}{{end}}' | sed 's/^,//'))
endef

define docker_config_bridge
	@set -e; \
		docker network ls --format "{{.Name}}" | grep '^$(1)$$' >/dev/null || exit 0; \
		set -o pipefail; \
		default=$$(docker network inspect $(1) -f '{{index .Options "com.docker.network.bridge.default_bridge"}}'); \
		br="docker0"; \
		[ "$$default" != "true" ] && br="br-$$(docker network inspect $(1) -f "{{.Id}}" | head -c 12)"; \
		docker run --rm --privileged --network=host $(REGISTRY)/kube-ovn:$(VERSION) bash -ec '\
			for brif in $$(ls /sys/class/net/'$$br'/brif); do \
				echo $(2) > /sys/class/net/'$$br'/brif/$$brif/hairpin_mode; \
			done'; \
		if [ -z "$(3)" ]; then \
			docker run --rm --privileged --network=host $(REGISTRY)/kube-ovn:$(VERSION) bash -ec '\
				echo 0 > /sys/class/net/'$$br'/bridge/vlan_filtering; \
			'; \
		else \
			docker run --rm --privileged --network=host $(REGISTRY)/kube-ovn:$(VERSION) bash -ec '\
				echo 1 > /sys/class/net/'$$br'/bridge/vlan_filtering; \
				bridge vlan show | awk "/^'$$br'/{print \$$2; while (getline > 0) {\
					if (\$$0 ~ /^[[:blank:]]/) {print \$$1} else {exit 0} }\
				}" | while read vid; do \
					bridge vlan del vid $$vid dev '$$br' self; \
				done; \
				bridge vlan add vid $(3) dev '$$br' pvid untagged self; \
				for brif in $$(ls /sys/class/net/'$$br'/brif); do \
					bridge vlan show | awk "/^$$brif/{print \$$2; while (getline > 0) {\
						if (\$$0 ~ /^[[:blank:]]/) {print \$$1} else {exit 0} }\
					}" | while read vid; do \
						bridge vlan del vid $$vid dev $$brif; \
					done; \
					bridge vlan add vid $(3) dev $$brif; \
				done'; \
		fi
endef

define add_docker_iptables_rule
	@sudo $(1) -t filter -C DOCKER-USER $(2) 2>/dev/null || sudo $(1) -I DOCKER-USER $(2)
endef

define kind_create_cluster
	kind create cluster --config $(1) --name $(2)
	@if [ "x$(3)" = "x1" ]; then \
		kubectl delete --ignore-not-found sc standard; \
		kubectl delete --ignore-not-found -n local-path-storage deploy local-path-provisioner; \
	fi
	kubectl describe no
endef

define kind_load_image
	@if [ "x$(3)" = "x1" ]; then \
		$(call docker_ensure_image_exists,$(2)); \
	fi
	kind load docker-image --name $(1) $(2)
endef

define kind_load_submariner_images
	$(call kind_load_image,$(1),$(SUBMARINER_OPERATOR),1)
	$(call kind_load_image,$(1),$(SUBMARINER_GATEWAY),1)
	$(call kind_load_image,$(1),$(SUBMARINER_LIGHTHOUSE_AGENT),1)
	$(call kind_load_image,$(1),$(SUBMARINER_LIGHTHOUSE_COREDNS),1)
	$(call kind_load_image,$(1),$(SUBMARINER_ROUTE_AGENT),1)
	$(call kind_load_image,$(1),$(SUBMARINER_NETTEST),1)
endef

define kind_load_kwok_image
	$(call kind_load_image,$(1),$(KWOK_IMAGE),1)
endef

define kubectl_wait_exist
	@echo "Waiting for $(2) $(1)/$(3) to be created..."
	@n=0; while ! kubectl -n "$(1)" get "$(2)" -o name | awk -F / '{print $$2}' | grep -q ^$(3)$$; do \
		test $$n -eq 60 && exit 1; \
		sleep 1; \
		n=$$(($$n+1)); \
	done
endef

define kubectl_wait_exist_and_ready
	$(call kubectl_wait_exist,$(1),$(2),$(3))
	kubectl -n $(1) rollout status --timeout=60s $(2) $(3)
endef

define kubectl_wait_submariner_ready
	$(call kubectl_wait_exist_and_ready,submariner-operator,deployment,submariner-operator)
	$(call kubectl_wait_exist_and_ready,submariner-operator,deployment,submariner-lighthouse-agent)
	$(call kubectl_wait_exist_and_ready,submariner-operator,deployment,submariner-lighthouse-coredns)
	$(call kubectl_wait_exist_and_ready,submariner-operator,daemonset,submariner-gateway)
	$(call kubectl_wait_exist_and_ready,submariner-operator,daemonset,submariner-metrics-proxy)
	$(call kubectl_wait_exist_and_ready,submariner-operator,daemonset,submariner-routeagent)
endef

.PHONY: check-kube-ovn-pod-restarts
check-kube-ovn-pod-restarts:
	bash hack/ci-check-crash.sh

.PHONY: uninstall
uninstall:
	bash dist/images/cleanup.sh

.PHONY: lint
lint:
    ifeq ($(CI),true)
		@echo "Running in GitHub Actions"
		golangci-lint run -v
    else
		@echo "Running in local environment"
		golangci-lint run -v --fix
    endif

.PHONY: lint-windows
lint-windows:
	@GOOS=windows go vet ./cmd/windows/...
	@GOOS=windows gosec ./pkg/util
	@GOOS=windows gosec ./pkg/request
	@GOOS=windows gosec ./cmd/cni

.PHONY: scan
scan:
	trivy image --exit-code=1 --ignore-unfixed --scanners vuln $(REGISTRY)/kube-ovn:$(RELEASE_TAG)
	trivy image --exit-code=1 --ignore-unfixed --scanners vuln $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG)

.PHONY: ut
ut:
	ginkgo -mod=mod --show-node-events --poll-progress-after=60s $(GINKGO_OUTPUT_OPT) -v test/unittest
	go test -coverprofile=profile.cov $$(go list ./pkg/... | grep -vw '^github.com/kubeovn/kube-ovn/pkg/client')

.PHONY: ovs-sandbox
ovs-sandbox: clean-ovs-sandbox
	docker run -itd --name ut-ovs-sandbox \
		--privileged \
		-v /tmp:/tmp \
		$(REGISTRY)/kube-ovn-base:$(RELEASE_TAG) ovs-sandbox -i

.PHONY: clean-ovs-sandbox
clean-ovs-sandbox:
	file /tmp/sandbox && docker rm -f ut-ovs-sandbox && rm -fr /tmp/sandbox

.PHONY: cp-ovs-ctl
cp-ovs-ctl:
	docker cp ut-ovs-sandbox:/usr/bin/ovs-vsctl /usr/bin/ovs-vsctl
	/usr/bin/ovs-vsctl --db=unix:/tmp/sandbox/db.sock show

.PHONY: cover
cover:
	go test ./pkg/ovs ./pkg/util ./pkg/ipam -gcflags=all=-l -coverprofile=cover.out -covermode=atomic
	go tool cover -func=cover.out | grep -v "100.0%"
	go tool cover -html=cover.out -o cover.html

.PHONY: ginkgo-cover
ginkgo-cover:
	if [ -f test/unittest/cover.out ]; then rm test/unittest/cover.out; fi
	cd test/unittest && ginkgo -r -cover -output-dir=. -coverprofile=cover.out -covermode=atomic -coverpkg=github.com/kubeovn/kube-ovn/pkg/ipam
	go tool cover -func=test/unittest/cover.out | grep -v "100.0%"
	go tool cover -html=test/unittest/cover.out -o test/unittest/cover.html

.PHONY: ipam-bench
ipam-bench:
	go test -timeout 30m -bench='^BenchmarkIPAM' -benchtime=10000x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false
	go test -timeout 90m -bench='^BenchmarkParallelIPAM' -benchtime=10x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false

.PHONY: kubectl-ko-log
kubectl-ko-log:
	bash dist/images/kubectl-ko log all
	tar -zcvf kubectl-ko-log.tar.gz kubectl-ko-log/

.PHONY: clean
clean:
	$(RM) dist/images/kube-ovn dist/images/kube-ovn-cmd
	$(RM) yamls/kind.yaml
	$(RM) yamls/clab-bgp.yaml yamls/clab-bgp-ha.yaml
	$(RM) ovn.yaml kube-ovn.yaml kube-ovn-crd.yaml
	$(RM) ovn-ic-config.yaml ovn-ic-0.yaml ovn-ic-1.yaml
	$(RM) kwok-node.yaml metallb-cr.yaml
	$(RM) cacert.pem ovn-req.pem ovn-cert.pem ovn-privkey.pem
	$(RM) kube-ovn.tar kube-ovn-dpdk.tar vpc-nat-gateway.tar image-amd64.tar image-amd64-dpdk.tar image-arm64.tar
	$(RM) kubectl-ko-log.tar.gz
	$(RM) -r kubectl-ko-log/

.PHONY: changelog
changelog:
	./hack/changelog.sh > CHANGELOG.md

.PHONY: local-dev
local-dev:
	@DEBUG=1 $(MAKE) build-go
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) --build-arg VERSION=$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway
	@$(MAKE) kind-init kind-install
