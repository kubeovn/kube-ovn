SHELL = /bin/bash

include Makefile.e2e

REGISTRY = kubeovn
DEV_TAG = dev
RELEASE_TAG = $(shell cat VERSION)
DEBUG_TAG = $(shell cat VERSION)-debug
LEGACY_TAG = $(shell cat VERSION)-amd64-legacy
VERSION = $(shell echo $${VERSION:-$(RELEASE_TAG)})
COMMIT = git-$(shell git rev-parse --short HEAD)
DATE = $(shell date +"%Y-%m-%d_%H:%M:%S")

GO_VERSION = $(shell echo $${GO_VERSION:-1.22.7})
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

FRR_VERSION = 9.1.1
FRR_IMAGE = quay.io/frrouting/frr:$(FRR_VERSION)

CLAB_IMAGE = ghcr.io/srl-labs/clab:0.57.2

MULTUS_VERSION = v4.1.1
MULTUS_IMAGE = ghcr.io/k8snetworkplumbingwg/multus-cni:$(MULTUS_VERSION)-thick
MULTUS_YAML = https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/$(MULTUS_VERSION)/deployments/multus-daemonset-thick.yml

METALLB_VERSION = 0.14.8
METALLB_CHART_REPO = https://metallb.github.io/metallb
METALLB_CONTROLLER_IMAGE = quay.io/metallb/controller:v$(METALLB_VERSION)
METALLB_SPEAKER_IMAGE = quay.io/metallb/speaker:v$(METALLB_VERSION)

KUBEVIRT_VERSION = v1.3.1
KUBEVIRT_OPERATOR_IMAGE = quay.io/kubevirt/virt-operator:$(KUBEVIRT_VERSION)
KUBEVIRT_API_IMAGE = quay.io/kubevirt/virt-api:$(KUBEVIRT_VERSION)
KUBEVIRT_CONTROLLER_IMAGE = quay.io/kubevirt/virt-controller:$(KUBEVIRT_VERSION)
KUBEVIRT_HANDLER_IMAGE = quay.io/kubevirt/virt-handler:$(KUBEVIRT_VERSION)
KUBEVIRT_LAUNCHER_IMAGE = quay.io/kubevirt/virt-launcher:$(KUBEVIRT_VERSION)
KUBEVIRT_OPERATOR_YAML = https://github.com/kubevirt/kubevirt/releases/download/$(KUBEVIRT_VERSION)/kubevirt-operator.yaml
KUBEVIRT_CR_YAML = https://github.com/kubevirt/kubevirt/releases/download/$(KUBEVIRT_VERSION)/kubevirt-cr.yaml

CILIUM_VERSION = 1.16.1
CILIUM_IMAGE_REPO = quay.io/cilium

CERT_MANAGER_VERSION = v1.15.2
CERT_MANAGER_CONTROLLER = quay.io/jetstack/cert-manager-controller:$(CERT_MANAGER_VERSION)
CERT_MANAGER_CAINJECTOR = quay.io/jetstack/cert-manager-cainjector:$(CERT_MANAGER_VERSION)
CERT_MANAGER_WEBHOOK = quay.io/jetstack/cert-manager-webhook:$(CERT_MANAGER_VERSION)
CERT_MANAGER_YAML = https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml

SUBMARINER_VERSION = $(shell echo $${SUBMARINER_VERSION:-0.18.0})
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

KWOK_VERSION = v0.6.0
KWOK_IMAGE = registry.k8s.io/kwok/kwok:$(KWOK_VERSION)

VPC_NAT_GW_IMG = $(REGISTRY)/vpc-nat-gateway:$(VERSION)

ANP_TEST_IMAGE = registry.k8s.io/e2e-test-images/agnhost:2.45
ANP_CR_YAML = https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/main/config/crd/standard/policy.networking.k8s.io_adminnetworkpolicies.yaml
BANP_CR_YAML = https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/main/config/crd/standard/policy.networking.k8s.io_baselineadminnetworkpolicies.yaml

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
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-pinger -v ./cmd/pinger
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
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GO_BUILD_FLAGS) -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-pinger -v ./cmd/pinger

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
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION=$(GO_VERSION) -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION=$(GO_VERSION) --build-arg LEGACY=true -t $(REGISTRY)/kube-ovn-base:$(LEGACY_TAG) -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 --build-arg GO_VERSION=$(GO_VERSION) --build-arg DEBUG=true -t $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-amd64 -o type=docker -f dist/images/Dockerfile.base dist/images/

.PHONY: base-amd64-dpdk
base-amd64-dpdk:
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o type=docker -f dist/images/Dockerfile.base-dpdk dist/images/

.PHONY: base-arm64
base-arm64:
	docker buildx build --platform linux/arm64 --build-arg ARCH=arm64 --build-arg GO_VERSION=$(GO_VERSION) -t $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 -o type=docker -f dist/images/Dockerfile.base dist/images/
	docker buildx build --platform linux/arm64 --build-arg ARCH=arm64 --build-arg GO_VERSION=$(GO_VERSION) --build-arg DEBUG=true -t $(REGISTRY)/kube-ovn-base:$(DEBUG_TAG)-arm64 -o type=docker -f dist/images/Dockerfile.base dist/images/


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

define docker_ensure_image_exists
	if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep "^$(1)$$" >/dev/null; then \
		docker pull "$(1)"; \
	fi
endef

define docker_rm_container
	@docker ps -a -f name="^$(1)$$" --format "{{.ID}}" | while read c; do docker rm -f $$c; done
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

define kubectl_wait_exist_and_ready
	@echo "Waiting for $(2) $(1)/$(3) to exist..."
	@n=0; while ! kubectl -n $(1) get $(2) -o name | awk -F / '{print $$2}' | grep -q ^$(3)$$; do \
		test $$n -eq 60 && exit 1; \
		sleep 1; \
		n=$$(($$n+1)); \
	done
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

define subctl_join
	@if [ $(OS_LINUX) -ne 1 ]; then \
		set -e; \
		docker exec $(1)-control-plane bash -c "command -v xz >/dev/null || (apt update && apt install -y xz-utils)"; \
		docker exec -e VERSION=v$(SUBMARINER_VERSION) -e DESTDIR=/usr/local/bin $(1)-control-plane bash -c "command -v subctl >/dev/null || curl -Ls https://get.submariner.io | bash"; \
		docker cp broker-info-internal.subm $(1)-control-plane:/broker-info-internal.subm; \
	fi

	kubectl config use-context kind-$(1)
	kubectl label --overwrite node $(1)-worker submariner.io/gateway=true
	@if [ $(OS_LINUX) -eq 1 ]; then \
		subctl join broker-info-internal.subm --clusterid $(2) --clustercidr $$(echo '$(3)' | tr ';' ',') --natt=false --cable-driver vxlan --health-check=false --context=kind-$(1); \
	else \
		docker exec $(1)-control-plane sh -c "subctl join /broker-info-internal.subm --clusterid $(2) --clustercidr $$(echo '$(3)' | tr ';' ',') --natt=false --cable-driver vxlan --health-check=false"; \
	fi
	$(call kubectl_wait_submariner_ready)
endef

.PHONY: kind-network-create-underlay
kind-network-create-underlay:
	$(eval UNDERLAY_NETWORK_ID = $(shell docker network ls -f name='^kind-underlay$$' --format '{{.ID}}'))
	@if [ -z "$(UNDERLAY_NETWORK_ID)" ]; then \
		docker network create --attachable -d bridge \
			--ipv6 --subnet fc00:adb1:b29b:608d::/64 --gateway fc00:adb1:b29b:608d::1 \
			-o com.docker.network.bridge.enable_ip_masquerade=true \
			-o com.docker.network.driver.mtu=1500 kind-underlay; \
	fi

.PHONY: kind-network-connect-underlay
kind-network-connect-underlay:
	@for node in `kind -n kube-ovn get nodes`; do \
		docker network connect kind-underlay $$node; \
		docker exec $$node ip address flush dev eth1; \
	done

.PHONY: kind-iptables-accepct-underlay
kind-iptables-accepct-underlay:
	$(call docker_network_info,kind)
	$(call docker_network_info,kind-underlay)
	$(call add_docker_iptables_rule,iptables,-s $(KIND_UNDERLAY_IPV4_SUBNET) -d $(KIND_IPV4_SUBNET) -j ACCEPT)
	$(call add_docker_iptables_rule,iptables,-d $(KIND_UNDERLAY_IPV4_SUBNET) -s $(KIND_IPV4_SUBNET) -j ACCEPT)
	$(call add_docker_iptables_rule,ip6tables,-s $(KIND_UNDERLAY_IPV6_SUBNET) -d $(KIND_IPV6_SUBNET) -j ACCEPT)
	$(call add_docker_iptables_rule,ip6tables,-d $(KIND_UNDERLAY_IPV6_SUBNET) -s $(KIND_IPV6_SUBNET) -j ACCEPT)

.PHONY: kind-generate-config
kind-generate-config:
	jinjanate yamls/kind.yaml.j2 -o yamls/kind.yaml

.PHONY: kind-disable-hairpin
kind-disable-hairpin:
	$(call docker_config_bridge,$(KIND_NETWORK_UNDERLAY),0,)

.PHONY: kind-enable-hairpin
kind-enable-hairpin:
	$(call docker_config_bridge,$(KIND_NETWORK_UNDERLAY),1,)

.PHONY: kind-create
kind-create:
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn,1)

.PHONY: kind-init
kind-init: kind-init-ipv4

.PHONY: kind-init-%
kind-init-%: kind-clean
	@auditing=$(KIND_AUDITING) ip_family=$* $(MAKE) kind-generate-config
	@$(MAKE) kind-create

.PHONY: kind-init-ovn-ic
kind-init-ovn-ic: kind-init-ovn-ic-ipv4

.PHONY: kind-init-ovn-ic-%
kind-init-ovn-ic-%: kind-clean-ovn-ic
	@n_worker=2 $(MAKE) kind-init-$*
	@n_worker=3 ip_family=$* auditing=$(KIND_AUDITING) $(MAKE) kind-generate-config
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn1,1)

.PHONY: kind-init-cilium-chaining
kind-init-cilium-chaining: kind-init-cilium-chaining-ipv4

.PHONY: kind-init-cilium-chaining-%
kind-init-cilium-chaining-%: kind-network-create-underlay
	@kube_proxy_mode=none $(MAKE) kind-init-$*
	@$(MAKE) kind-iptables-accepct-underlay
	@$(MAKE) kind-network-connect-underlay

.PHONY: kind-init-ovn-submariner
kind-init-ovn-submariner: kind-clean-ovn-submariner kind-init
	@pod_cidr_v4=10.18.0.0/16 svc_cidr_v4=10.112.0.0/12 $(MAKE) kind-generate-config
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn1,1)

.PHONY: kind-init-deepflow
kind-init-deepflow: kind-clean
	@mapped_ports=$(DEEPFLOW_MAPPED_PORTS) $(MAKE) kind-generate-config
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn,0)

.PHONY: kind-init-iptables
kind-init-iptables:
	@kube_proxy_mode=iptables $(MAKE) kind-init

.PHONY: kind-init-ha
kind-init-ha: kind-init-ha-ipv4

.PHONY: kind-init-ha-%
kind-init-ha-%:
	@ha=true $(MAKE) kind-init-$*

.PHONY: kind-init-single
kind-init-single: kind-init-single-ipv4

.PHONY: kind-init-single-%
kind-init-single-%:
	@single=true $(MAKE) kind-init-$*

.PHONY: kind-init-bgp
kind-init-bgp: kind-clean-bgp kind-init
	kube_ovn_version=$(VERSION) frr_image=$(FRR_IMAGE) jinjanate yamls/clab-bgp.yaml.j2 -o yamls/clab-bgp.yaml
	docker run --rm --privileged \
		--name kube-ovn-bgp \
		--network host \
		--pid host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /var/run/netns:/var/run/netns \
		-v /var/lib/docker/containers:/var/lib/docker/containers \
		-v $(CURDIR)/yamls/clab-bgp.yaml:/clab-bgp/clab.yaml \
		$(CLAB_IMAGE) clab deploy -t /clab-bgp/clab.yaml

.PHONY: kind-init-bgp-ha
kind-init-bgp-ha: kind-clean-bgp kind-init
	kube_ovn_version=$(VERSION) frr_image=$(FRR_IMAGE) jinjanate yamls/clab-bgp-ha.yaml.j2 -o yamls/clab-bgp-ha.yaml
	docker run --rm --privileged \
		--name kube-ovn-bgp \
		--network host \
		--pid host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /var/run/netns:/var/run/netns \
		-v /var/lib/docker/containers:/var/lib/docker/containers \
		-v $(CURDIR)/yamls/clab-bgp-ha.yaml:/clab-bgp/clab.yaml \
		$(CLAB_IMAGE) clab deploy -t /clab-bgp/clab.yaml

.PHONY: kind-load-image
kind-load-image:
	$(call kind_load_image,kube-ovn,$(REGISTRY)/kube-ovn:$(VERSION))

.PHONY: kind-untaint-control-plane
kind-untaint-control-plane:
	@for node in $(shell kubectl get no -o jsonpath='{.items[*].metadata.name}'); do \
		for key in $(CONTROL_PLANE_TAINTS); do \
			taint=$$(kubectl get no $$node -o jsonpath="{.spec.taints[?(@.key==\"$$key\")]}"); \
			if [ -n "$$taint" ]; then \
				kubectl taint node $$node $$key:NoSchedule-; \
			fi; \
		done; \
	done

.PHONY: kind-install-chart
kind-install-chart: kind-load-image kind-untaint-control-plane
	kubectl label node -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
	kubectl label node -lnode-role.kubernetes.io/control-plane kube-ovn/role=master --overwrite
	kubectl label node -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel --overwrite
	helm install kubeovn ./charts/kube-ovn --wait \
		--set global.images.kubeovn.tag=$(VERSION) \
		--set networking.NET_STACK=$(shell echo $${NET_STACK:-ipv4} | sed 's/^dual$$/dual_stack/') \
		--set networking.ENABLE_SSL=$(shell echo $${ENABLE_SSL:-false}) \
		--set func.ENABLE_BIND_LOCAL_IP=$(shell echo $${ENABLE_BIND_LOCAL_IP:-true}) \
		--set func.ENABLE_OVN_IPSEC=$(shell echo $${ENABLE_OVN_IPSEC:-false}) \
		--set func.ENABLE_ANP=$(shell echo $${ENABLE_ANP:-false}) \
		--set func.ENABLE_IC=$(shell kubectl get node --show-labels | grep -qw "ovn.kubernetes.io/ic-gw" && echo true || echo false)

.PHONY: kind-install-chart-ssl
kind-install-chart-ssl:
	@ENABLE_SSL=true $(MAKE) kind-install-chart

.PHONY: kind-upgrade-chart
kind-upgrade-chart: kind-load-image
	helm upgrade kubeovn ./charts/kube-ovn --wait \
		--set global.images.kubeovn.tag=$(VERSION) \
		--set networking.NET_STACK=$(shell echo $${NET_STACK:-ipv4} | sed 's/^dual$$/dual_stack/') \
		--set networking.ENABLE_SSL=$(shell echo $${ENABLE_SSL:-false}) \
		--set func.ENABLE_BIND_LOCAL_IP=$(shell echo $${ENABLE_BIND_LOCAL_IP:-true}) \
		--set func.ENABLE_OVN_IPSEC=$(shell echo $${ENABLE_OVN_IPSEC:-false}) \
		--set func.ENABLE_ANP=$(shell echo $${ENABLE_ANP:-false}) \
		--set func.ENABLE_IC=$(shell kubectl get node --show-labels | grep -qw "ovn.kubernetes.io/ic-gw" && echo true || echo false)
	kubectl -n kube-system wait pod --for=condition=ready -l app=ovs --timeout=60s

.PHONY: kind-uninstall-chart
kind-uninstall-chart:
	helm uninstall kubeovn

.PHONY: kind-install
kind-install: kind-load-image
	kubectl config use-context kind-kube-ovn
	@$(MAKE) kind-untaint-control-plane
	sed 's/VERSION=.*/VERSION=$(VERSION)/' dist/images/install.sh | bash
	kubectl describe no

.PHONY: kind-install-ipv4
kind-install-ipv4: kind-install

.PHONY: kind-install-ipv6
kind-install-ipv6:
	@IPV6=true $(MAKE) kind-install

.PHONY: kind-install-dual
kind-install-dual:
	@DUAL_STACK=true $(MAKE) kind-install

.PHONY: kind-install-overlay-%
kind-install-overlay-%:
	@$(MAKE) kind-install-$*

.PHONY: kind-install-dev
kind-install-dev: kind-install-dev-ipv4

.PHONY: kind-install-dev-%
kind-install-dev-%:
	@VERSION=$(DEV_TAG) $(MAKE) kind-install-$*

.PHONY: kind-install-debug
kind-install-debug: kind-install-debug-ipv4

.PHONY: kind-install-debug-%
kind-install-debug-%:
	@VERSION=$(DEBUG_TAG) $(MAKE) kind-install-$*

.PHONY: kind-install-debug-valgrind
kind-install-debug-valgrind: kind-install-debug-valgrind-ipv4
	@DEBUG_WRAPPER=valgrind $(MAKE) kind-install-debug

.PHONY: kind-install-debug-valgrind-%
kind-install-debug-valgrind-%:
	@DEBUG_WRAPPER=valgrind $(MAKE) kind-install-debug-$*

.PHONY: kind-install-ovn-ic
kind-install-ovn-ic: kind-install-ovn-ic-ipv4

.PHONY: kind-install-ovn-ic-ipv4
kind-install-ovn-ic-ipv4:
	@ENABLE_IC=true $(MAKE) kind-install
	$(call kind_load_image,kube-ovn1,$(REGISTRY)/kube-ovn:$(VERSION))
	kubectl config use-context kind-kube-ovn1
	@$(MAKE) kind-untaint-control-plane
	sed -e 's/10.16.0/10.18.0/g' \
		-e 's/10.96.0/10.98.0/g' \
		-e 's/100.64.0/100.68.0/g' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | ENABLE_IC=true bash
	kubectl describe no

	kubectl config use-context kind-kube-ovn
	sed 's/VERSION=.*/VERSION=$(VERSION)/' dist/images/install-ic-server.sh | bash

	@$(MAKE) kind-config-ovn-ic

define kind_config_ovn_ic
	kubectl config use-context kind-$(1)
	$(eval IC_GATEWAY_NODES=$(shell kind get nodes -n $(1) | sort -r | head -n3 | tr '\n' ',' | sed 's/,$$//'))
	ic_db_host=$(2) zone=$(3) gateway_nodes=$(IC_GATEWAY_NODES) jinjanate yamls/ovn-ic-config.yaml.j2 -o ovn-ic-config.yaml
	kubectl apply -f ovn-ic-config.yaml
endef

.PHONY: kind-config-ovn-ic
kind-config-ovn-ic:
	$(eval IC_DB_IPS=$(shell kubectl config use-context kind-kube-ovn >/dev/null && kubectl get deploy/ovn-ic-server -n kube-system -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="NODE_IPS")].value}'))
	$(call kind_config_ovn_ic,kube-ovn,$(IC_DB_IPS),az0)
	$(call kind_config_ovn_ic,kube-ovn1,$(IC_DB_IPS),az1)

.PHONY: kind-install-ovn-ic-ipv6
kind-install-ovn-ic-ipv6:
	@ENABLE_IC=true $(MAKE) kind-install-ipv6
	$(call kind_load_image,kube-ovn1,$(REGISTRY)/kube-ovn:$(VERSION))
	kubectl config use-context kind-kube-ovn1
	@$(MAKE) kind-untaint-control-plane
	sed -e 's/fd00:10:16:/fd00:10:18:/g' \
		-e 's/fd00:10:96:/fd00:10:98:/g' \
		-e 's/fd00:100:64:/fd00:100:68:/g' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		IPV6=true ENABLE_IC=true bash
	kubectl describe no

	kubectl config use-context kind-kube-ovn
	sed 's/VERSION=.*/VERSION=$(VERSION)/' dist/images/install-ic-server.sh | bash

	@$(MAKE) kind-config-ovn-ic

.PHONY: kind-install-ovn-ic-dual
kind-install-ovn-ic-dual:
	@ENABLE_IC=true $(MAKE) kind-install-dual
	$(call kind_load_image,kube-ovn1,$(REGISTRY)/kube-ovn:$(VERSION))
	kubectl config use-context kind-kube-ovn1
	@$(MAKE) kind-untaint-control-plane
	sed -e 's/10.16.0/10.18.0/g' \
		-e 's/10.96.0/10.98.0/g' \
		-e 's/100.64.0/100.68.0/g' \
		-e 's/fd00:10:16:/fd00:10:18:/g' \
		-e 's/fd00:10:96:/fd00:10:98:/g' \
		-e 's/fd00:100:64:/fd00:100:68:/g' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_IC=true bash
	kubectl describe no

	kubectl config use-context kind-kube-ovn
	sed 's/VERSION=.*/VERSION=$(VERSION)/' dist/images/install-ic-server.sh | bash

	@$(MAKE) kind-config-ovn-ic

.PHONY: kind-install-ovn-submariner
kind-install-ovn-submariner: kind-install
	$(call kind_load_submariner_images,kube-ovn)
	$(call kind_load_submariner_images,kube-ovn1)
	$(call kind_load_image,kube-ovn1,$(REGISTRY)/kube-ovn:$(VERSION))

	kubectl config use-context kind-kube-ovn1
	@$(MAKE) kind-untaint-control-plane
	sed -e 's/10.16.0/10.18.0/g' \
		-e 's/10.96.0.0/10.112.0.0/g' \
		-e 's/100.64.0/100.68.0/g' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | bash
	kubectl describe no

	kubectl config use-context kind-kube-ovn
	subctl deploy-broker
	cat broker-info.subm | base64 -d | \
		jq '.brokerURL = "https://$(shell docker inspect --format='{{.NetworkSettings.Networks.kind.IPAddress}}' kube-ovn-control-plane):6443"' | \
		base64 > broker-info-internal.subm

	$(call subctl_join,kube-ovn,cluster0,100.64.0.0/16;10.16.0.0/16)
	$(call subctl_join,kube-ovn1,cluster1,100.68.0.0/16;10.18.0.0/16)

.PHONY: kind-install-underlay
kind-install-underlay: kind-install-underlay-ipv4

.PHONY: kind-install-underlay-hairpin
kind-install-underlay-hairpin: kind-install-underlay-hairpin-ipv4

.PHONY: kind-install-underlay-ipv4
kind-install-underlay-ipv4: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_IPV4_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_IPV4_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_IPV4_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		ENABLE_VLAN=true VLAN_NIC=$(VLAN_NIC) bash
	kubectl describe no

.PHONY: kind-install-underlay-hairpin-ipv4
kind-install-underlay-hairpin-ipv4: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_IPV4_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_IPV4_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_IPV4_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		ENABLE_VLAN=true VLAN_NIC=$(VLAN_NIC) bash
	kubectl describe no

.PHONY: kind-install-underlay-ipv6
kind-install-underlay-ipv6: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		IPV6=true ENABLE_VLAN=true VLAN_NIC=$(VLAN_NIC) bash

.PHONY: kind-install-underlay-hairpin-ipv6
kind-install-underlay-hairpin-ipv6: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		IPV6=true ENABLE_VLAN=true VLAN_NIC=$(VLAN_NIC) bash

.PHONY: kind-install-underlay-dual
kind-install-underlay-dual: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_IPV4_SUBNET)),$($(UNDERLAY_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_IPV4_GATEWAY)),$($(UNDERLAY_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_IPV4_EXCLUDE_IPS)),$($(UNDERLAY_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=$(VLAN_NIC) bash

.PHONY: kind-install-underlay-hairpin-dual
kind-install-underlay-hairpin-dual: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_IPV4_SUBNET)),$($(UNDERLAY_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_IPV4_GATEWAY)),$($(UNDERLAY_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_IPV4_EXCLUDE_IPS)),$($(UNDERLAY_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=$(VLAN_NIC) bash

.PHONY: kind-install-underlay-u2o
kind-install-underlay-u2o: kind-install-underlay-u2o-ipv4

.PHONY: kind-install-underlay-u2o-%
kind-install-underlay-u2o-%:
	@$(MAKE) U2O_INTERCONNECTION=true kind-install-underlay-$*

.PHONY: kind-install-underlay-logical-gateway-dual
kind-install-underlay-logical-gateway-dual: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY))
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_IPV4_SUBNET)),$($(UNDERLAY_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_IPV4_GATEWAY))9,$($(UNDERLAY_IPV6_GATEWAY))f"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_IPV4_GATEWAY)),$($(UNDERLAY_IPV4_EXCLUDE_IPS)),$($(UNDERLAY_IPV6_GATEWAY)),$($(UNDERLAY_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true \
		VLAN_NIC=$(VLAN_NIC) LOGICAL_GATEWAY=true bash

.PHONY: kind-install-multus
kind-install-multus:
	$(call kind_load_image,kube-ovn,$(MULTUS_IMAGE),1)
	curl -s "$(MULTUS_YAML)" | sed 's/:snapshot-thick/:$(MULTUS_VERSION)-thick/g' | kubectl apply -f -
	kubectl -n kube-system rollout status ds kube-multus-ds

.PHONY: kind-install-metallb
kind-install-metallb:
	$(call docker_network_info,kind)
	$(call kind_load_image,kube-ovn,$(METALLB_CONTROLLER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(METALLB_SPEAKER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(FRR_IMAGE),1)
	helm repo add metallb $(METALLB_CHART_REPO)
	helm repo update metallb
	helm install metallb metallb/metallb --wait \
		--version $(METALLB_VERSION) \
		--namespace metallb-system \
		--create-namespace \
		--set speaker.frr.image.tag=$(FRR_VERSION)
	$(call kubectl_wait_exist_and_ready,metallb-system,deployment,metallb-controller)
	$(call kubectl_wait_exist_and_ready,metallb-system,daemonset,metallb-speaker)
	@metallb_pool=$(shell echo $(KIND_IPV4_SUBNET) | sed 's/.[^.]\+$$/.201/')-$(shell echo $(KIND_IPV4_SUBNET) | sed 's/.[^.]\+$$/.250/') \
		jinjanate yamls/metallb-cr.yaml.j2 -o metallb-cr.yaml
	kubectl apply -f metallb-cr.yaml

.PHONY: kind-install-vpc-nat-gw
kind-install-vpc-nat-gw:
	$(call kind_load_image,kube-ovn,$(VPC_NAT_GW_IMG))
	@$(MAKE) ENABLE_NAT_GW=true CNI_CONFIG_PRIORITY=10 kind-install
	@$(MAKE) kind-install-multus

.PHONY: kind-install-kubevirt
kind-install-kubevirt:
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_OPERATOR_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_API_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_CONTROLLER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_HANDLER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_LAUNCHER_IMAGE),1)

	kubectl apply -f "$(KUBEVIRT_OPERATOR_YAML)"
	kubectl apply -f "$(KUBEVIRT_CR_YAML)"
	kubectl -n kubevirt scale deploy virt-operator --replicas=1
	kubectl -n kubevirt patch kubevirt kubevirt --type=merge --patch \
		'{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}},"infra":{"replicas":1}}}'
	$(call kubectl_wait_exist_and_ready,kubevirt,deployment,virt-operator)
	$(call kubectl_wait_exist_and_ready,kubevirt,deployment,virt-api)
	$(call kubectl_wait_exist_and_ready,kubevirt,deployment,virt-controller)
	$(call kubectl_wait_exist_and_ready,kubevirt,daemonset,virt-handler)

.PHONY: kind-install-lb-svc
kind-install-lb-svc:
	$(call kind_load_image,kube-ovn,$(VPC_NAT_GW_IMG))
	@$(MAKE) ENABLE_LB_SVC=true CNI_CONFIG_PRIORITY=10 kind-install
	@$(MAKE) kind-install-multus

.PHONY: kind-install-webhook
kind-install-webhook: kind-install
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_CONTROLLER),1)
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_CAINJECTOR),1)
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_WEBHOOK),1)

	kubectl apply -f "$(CERT_MANAGER_YAML)"
	kubectl rollout status deployment/cert-manager -n cert-manager --timeout 120s
	kubectl rollout status deployment/cert-manager-cainjector -n cert-manager --timeout 120s
	kubectl rollout status deployment/cert-manager-webhook -n cert-manager --timeout 120s

	sed 's#image: .*#image: $(REGISTRY)/kube-ovn:$(VERSION)#' yamls/webhook.yaml | kubectl apply -f -
	kubectl rollout status deployment/kube-ovn-webhook -n kube-system --timeout 120s

.PHONY: kind-install-cilium-chaining
kind-install-cilium-chaining: kind-install-cilium-chaining-ipv4

.PHONY: kind-install-cilium-chaining-%
kind-install-cilium-chaining-%:
	$(eval KUBERNETES_SERVICE_HOST = $(shell kubectl get nodes kube-ovn-control-plane -o jsonpath='{.status.addresses[0].address}'))
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO)/cilium:v$(CILIUM_VERSION),1)
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO)/operator-generic:v$(CILIUM_VERSION),1)
	kubectl apply -f yamls/cilium-chaining.yaml
	helm repo add cilium https://helm.cilium.io/
	helm repo update cilium
	helm install cilium cilium/cilium --wait \
		--version $(CILIUM_VERSION) \
		--namespace kube-system \
		--set k8sServiceHost=$(KUBERNETES_SERVICE_HOST) \
		--set k8sServicePort=6443 \
		--set kubeProxyReplacement=false \
		--set operator.replicas=1 \
		--set socketLB.enabled=true \
		--set nodePort.enabled=true \
		--set externalIPs.enabled=true \
		--set hostPort.enabled=false \
		--set sessionAffinity=true \
		--set enableIPv4Masquerade=false \
		--set enableIPv6Masquerade=false \
		--set hubble.enabled=true \
		--set sctp.enabled=true \
		--set ipv4.enabled=$(shell if echo $* | grep -q ipv6; then echo false; else echo true; fi) \
		--set ipv6.enabled=$(shell if echo $* | grep -q ipv4; then echo false; else echo true; fi) \
		--set routingMode=native \
		--set devices="eth+ ovn0 genev_sys_6081 vxlan_sys_4789" \
		--set forceDeviceDetection=true \
		--set ipam.mode=cluster-pool \
		--set-json ipam.operator.clusterPoolIPv4PodCIDRList='["100.65.0.0/16"]' \
		--set-json ipam.operator.clusterPoolIPv6PodCIDRList='["fd00:100:65::/112"]' \
		--set cni.chainingMode=generic-veth \
		--set cni.chainingTarget=kube-ovn \
		--set cni.customConf=true \
		--set cni.configMap=cni-configuration
	kubectl -n kube-system rollout status ds cilium --timeout 120s
	@$(MAKE) ENABLE_LB=false ENABLE_NP=false \
		CNI_CONFIG_PRIORITY=10 WITHOUT_KUBE_PROXY=true \
		KIND_NETWORK_UNDERLAY=kind-underlay \
		kind-install-$*
	kubectl describe no

.PHONY: kind-install-bgp
kind-install-bgp: kind-install
	kubectl label node --all ovn.kubernetes.io/bgp=true
	kubectl annotate subnet ovn-default ovn.kubernetes.io/bgp=local
	sed -e 's#image: .*#image: $(REGISTRY)/kube-ovn:$(VERSION)#' \
		-e 's/--neighbor-address=.*/--neighbor-address=10.0.1.1/' \
		-e 's/--neighbor-as=.*/--neighbor-as=65001/' \
		-e 's/--cluster-as=.*/--cluster-as=65002/' yamls/speaker.yaml | \
		kubectl apply -f -
	kubectl -n kube-system rollout status ds kube-ovn-speaker --timeout 60s
	docker exec clab-bgp-router vtysh -c "show ip route bgp"

.PHONY: kind-install-bgp-ha
kind-install-bgp-ha: kind-install
	kubectl label node --all ovn.kubernetes.io/bgp=true
	kubectl annotate subnet ovn-default ovn.kubernetes.io/bgp=local
	sed -e 's#image: .*#image: $(REGISTRY)/kube-ovn:$(VERSION)#' \
		-e 's/--neighbor-address=.*/--neighbor-address=10.0.1.1,10.0.1.2/' \
		-e 's/--neighbor-as=.*/--neighbor-as=65001/' \
		-e 's/--cluster-as=.*/--cluster-as=65002/' yamls/speaker.yaml | \
		kubectl apply -f -
	kubectl -n kube-system rollout status ds kube-ovn-speaker --timeout 60s
	docker exec clab-bgp-router-1 vtysh -c "show ip route bgp"
	docker exec clab-bgp-router-2 vtysh -c "show ip route bgp"

.PHONY: kind-install-deepflow
kind-install-deepflow: kind-install
	helm repo add deepflow $(DEEPFLOW_CHART_REPO)
	helm repo update deepflow
	$(eval CLICKHOUSE_PERSISTENCE = $(shell helm show values --version $(DEEPFLOW_CHART_VERSION) --jsonpath '{.clickhouse.storageConfig.persistence}' deepflow/deepflow | sed 's/0Gi/Gi/g'))
	helm install deepflow deepflow/deepflow --wait \
		--version $(DEEPFLOW_CHART_VERSION) \
		--namespace deepflow \
		--create-namespace \
		--set global.image.repository=$(DEEPFLOW_IMAGE_REPO) \
		--set global.image.pullPolicy=IfNotPresent \
		--set deepflow-agent.clusterNAME=kind-kube-ovn \
		--set grafana.image.registry=$(DEEPFLOW_IMAGE_REPO) \
		--set grafana.image.pullPolicy=IfNotPresent \
		--set grafana.service.nodePort=$(DEEPFLOW_GRAFANA_NODE_PORT) \
		--set mysql.storageConfig.persistence.size=5Gi \
		--set mysql.image.pullPolicy=IfNotPresent \
		--set clickhouse.image.pullPolicy=IfNotPresent \
		--set-json 'clickhouse.storageConfig.persistence=$(CLICKHOUSE_PERSISTENCE)'
	echo -e "\nGrafana URL: http://127.0.0.1:$(DEEPFLOW_GRAFANA_NODE_PORT)\nGrafana auth: admin:deepflow\n"

.PHONY: kind-install-deepflow-ctl
kind-install-deepflow-ctl:
	curl -so /usr/local/bin/deepflow-ctl $(DEEPFLOW_CTL_URL)
	chmod a+x /usr/local/bin/deepflow-ctl
	/usr/local/bin/deepflow-ctl -v

.PHONY: kind-install-kwok
kind-install-kwok:
	kubectl -n kube-system patch ds kube-proxy -p '{"spec":{"template":{"spec":{"nodeSelector":{"type":"kind"}}}}}'
	kubectl -n kube-system patch ds ovs-ovn -p '{"spec":{"template":{"spec":{"nodeSelector":{"type":"kind"}}}}}'
	kubectl -n kube-system patch ds kube-ovn-cni -p '{"spec":{"template":{"spec":{"nodeSelector":{"type":"kind"}}}}}'
	kubectl -n kube-system patch ds kube-ovn-pinger -p '{"spec":{"template":{"spec":{"nodeSelector":{"type":"kind"}}}}}'
	kubectl -n kube-system patch deploy kube-ovn-monitor -p '{"spec":{"template":{"spec":{"nodeSelector":{"type":"kind"}}}}}'
	kubectl -n kube-system patch deploy coredns -p '{"spec":{"template":{"spec":{"nodeSelector":{"type":"kind"}}}}}'
	$(call kind_load_kwok_image,kube-ovn)
	kubectl apply -f yamls/kwok.yaml
	kubectl apply -f yamls/kwok-stage.yaml
	kubectl -n kube-system rollout status deploy kwok-controller --timeout 60s
	for i in {1..20}; do \
		kwok_node_name=fake-node-$$i jinjanate yamls/kwok-node.yaml.j2 -o kwok-node.yaml; \
		kubectl apply -f kwok-node.yaml; \
	done

.PHONY: kind-install-ovn-ipsec
kind-install-ovn-ipsec:
	@$(MAKE) ENABLE_OVN_IPSEC=true kind-install

.PHONY: kind-install-anp
kind-install-anp: kind-load-image
	$(call kind_load_image,kube-ovn,$(ANP_TEST_IMAGE),1)
	kubectl apply -f "$(ANP_CR_YAML)"
	kubectl apply -f "$(BANP_CR_YAML)"
	@$(MAKE) ENABLE_ANP=true kind-install

.PHONY: kind-reload
kind-reload: kind-reload-ovs
	kubectl delete pod -n kube-system -l app=kube-ovn-controller
	kubectl delete pod -n kube-system -l app=kube-ovn-cni
	kubectl delete pod -n kube-system -l app=kube-ovn-pinger

.PHONY: kind-reload-ovs
kind-reload-ovs: kind-load-image
	kubectl -n kube-system rollout restart ds ovs-ovn

.PHONY: kind-clean
kind-clean:
	kind delete cluster --name=kube-ovn

.PHONY: kind-clean-ovn-ic
kind-clean-ovn-ic: kind-clean
	$(call docker_rm_container,ovn-ic-db)
	kind delete cluster --name=kube-ovn1

.PHONY: kind-clean-ovn-submariner
kind-clean-ovn-submariner: kind-clean
	kind delete cluster --name=kube-ovn1

.PHONY: kind-clean-bgp
kind-clean-bgp: kind-clean-bgp-ha
	kube_ovn_version=$(VERSION) frr_image=$(FRR_IMAGE) jinjanate yamls/clab-bgp.yaml.j2 -o yamls/clab-bgp.yaml
	docker run --rm --privileged \
		--name kube-ovn-bgp \
		--network host \
		--pid host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /var/run/netns:/var/run/netns \
		-v /var/lib/docker/containers:/var/lib/docker/containers \
		-v $(CURDIR)/yamls/clab-bgp.yaml:/clab-bgp/clab.yaml \
		$(CLAB_IMAGE) clab destroy -t /clab-bgp/clab.yaml
	@$(MAKE) kind-clean

.PHONY: kind-clean-bgp-ha
kind-clean-bgp-ha:
	kube_ovn_version=$(VERSION) frr_image=$(FRR_IMAGE) jinjanate yamls/clab-bgp-ha.yaml.j2 -o yamls/clab-bgp-ha.yaml
	docker run --rm --privileged \
		--name kube-ovn-bgp \
		--network host \
		--pid host \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /var/run/netns:/var/run/netns \
		-v /var/lib/docker/containers:/var/lib/docker/containers \
		-v $(CURDIR)/yamls/clab-bgp-ha.yaml:/clab-bgp/clab.yaml \
		$(CLAB_IMAGE) clab destroy -t /clab-bgp/clab.yaml
	@$(MAKE) kind-clean

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
	go test ./pkg/...

.PHONY: cover
cover:
	go test ./pkg/ovs ./pkg/util ./pkg/ipam -gcflags=all=-l -p 1 -coverprofile=cover.out -covermode=atomic
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
