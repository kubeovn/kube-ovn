SHELL = /bin/bash

include Makefile.e2e

REGISTRY = kubeovn
DEV_TAG = dev
RELEASE_TAG = $(shell cat VERSION)
VERSION = $(shell echo $${VERSION:-$(RELEASE_TAG)})
COMMIT = git-$(shell git rev-parse --short HEAD)
DATE = $(shell date +"%Y-%m-%d_%H:%M:%S")
GOLDFLAGS = "-w -s -extldflags '-z now' -X github.com/kubeovn/kube-ovn/versions.COMMIT=$(COMMIT) -X github.com/kubeovn/kube-ovn/versions.VERSION=$(RELEASE_TAG) -X github.com/kubeovn/kube-ovn/versions.BUILDDATE=$(DATE)"

OS_LINUX = 0
ifneq ($(OS),Windows_NT)
ifeq ($(shell uname -s),Linux)
OS_LINUX = 1
endif
endif

CONTROL_PLANE_TAINTS = node-role.kubernetes.io/master node-role.kubernetes.io/control-plane

CHART_UPGRADE_RESTART_OVS=$(shell echo $${CHART_UPGRADE_RESTART_OVS:-false})

MULTUS_IMAGE = ghcr.io/k8snetworkplumbingwg/multus-cni:snapshot-thick
MULTUS_YAML = https://raw.githubusercontent.com/k8snetworkplumbingwg/multus-cni/master/deployments/multus-daemonset-thick.yml

KUBEVIRT_VERSION = v0.58.1
KUBEVIRT_OPERATOR_IMAGE = quay.io/kubevirt/virt-operator:$(KUBEVIRT_VERSION)
KUBEVIRT_API_IMAGE = quay.io/kubevirt/virt-api:$(KUBEVIRT_VERSION)
KUBEVIRT_CONTROLLER_IMAGE = quay.io/kubevirt/virt-controller:$(KUBEVIRT_VERSION)
KUBEVIRT_HANDLER_IMAGE = quay.io/kubevirt/virt-handler:$(KUBEVIRT_VERSION)
KUBEVIRT_LAUNCHER_IMAGE = quay.io/kubevirt/virt-launcher:$(KUBEVIRT_VERSION)
KUBEVIRT_TEST_IMAGE = quay.io/kubevirt/cirros-container-disk-demo
KUBEVIRT_OPERATOR_YAML = https://github.com/kubevirt/kubevirt/releases/download/$(KUBEVIRT_VERSION)/kubevirt-operator.yaml
KUBEVIRT_CR_YAML = https://github.com/kubevirt/kubevirt/releases/download/$(KUBEVIRT_VERSION)/kubevirt-cr.yaml
KUBEVIRT_TEST_YAML = https://kubevirt.io/labs/manifests/vm.yaml

CILIUM_VERSION = 1.12.9
CILIUM_IMAGE_REPO = quay.io/cilium/cilium

CERT_MANAGER_VERSION = v1.11.1
CERT_MANAGER_CONTROLLER = quay.io/jetstack/cert-manager-controller:$(CERT_MANAGER_VERSION)
CERT_MANAGER_CAINJECTOR = quay.io/jetstack/cert-manager-cainjector:$(CERT_MANAGER_VERSION)
CERT_MANAGER_WEBHOOK = quay.io/jetstack/cert-manager-webhook:$(CERT_MANAGER_VERSION)
CERT_MANAGER_YAML = https://github.com/cert-manager/cert-manager/releases/download/$(CERT_MANAGER_VERSION)/cert-manager.yaml

SUBMARINER_VERSION = $(shell echo $${SUBMARINER_VERSION:-0.14.3})
SUBMARINER_OPERATOR = quay.io/submariner/submariner-operator:$(SUBMARINER_VERSION)
SUBMARINER_GATEWAY = quay.io/submariner/submariner-gateway:$(SUBMARINER_VERSION)
SUBMARINER_LIGHTHOUSE_AGENT = quay.io/submariner/lighthouse-agent:$(SUBMARINER_VERSION)
SUBMARINER_LIGHTHOUSE_COREDNS = quay.io/submariner/lighthouse-coredns:$(SUBMARINER_VERSION)
SUBMARINER_ROUTE_AGENT = quay.io/submariner/submariner-route-agent:$(SUBMARINER_VERSION)
SUBMARINER_NETTEST = quay.io/submariner/nettest:$(SUBMARINER_VERSION)

VPC_NAT_GW_IMG = $(REGISTRY)/vpc-nat-gateway:$(VERSION)

E2E_NETWORK = bridge
ifneq ($(VLAN_ID),)
	E2E_NETWORK = kube-ovn-vlan
endif

# ARCH could be amd64,arm64
ARCH = amd64

.PHONY: build-go
build-go:
	go mod tidy
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -ldflags $(GOLDFLAGS) -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-webhook -ldflags $(GOLDFLAGS) -v ./cmd/webhook
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o $(CURDIR)/dist/images/test-server -ldflags $(GOLDFLAGS) -v ./test/server

.PHONY: build-go-windows
build-go-windows:
	go mod tidy
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/windows/kube-ovn.exe -ldflags $(GOLDFLAGS) -v ./cmd/windows/cni
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -buildmode=pie -o $(CURDIR)/dist/windows/kube-ovn-daemon.exe -ldflags $(GOLDFLAGS) -v ./cmd/windows/daemon

.PHONY: build-go-arm
build-go-arm:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-cmd -ldflags $(GOLDFLAGS) -v ./cmd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildmode=pie -o $(CURDIR)/dist/images/kube-ovn-webhook -ldflags $(GOLDFLAGS) -v ./cmd/webhook

.PHONY: build-kube-ovn
build-kube-ovn: build-go
	docker build -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) -f dist/images/Dockerfile dist/images/
	docker build -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-no-avx512 -f dist/images/Dockerfile.no-avx512 dist/images/
	docker build -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk -f dist/images/Dockerfile.dpdk dist/images/

.PHONY: build-dev
build-dev: build-go
	docker build -t $(REGISTRY)/kube-ovn:$(DEV_TAG) -f dist/images/Dockerfile dist/images/

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

.PHONY: image-kube-ovn
image-kube-ovn: build-go
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-no-avx512 -o type=docker -f dist/images/Dockerfile.no-avx512 dist/images/
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk -o type=docker -f dist/images/Dockerfile.dpdk dist/images/

.PHONY: image-debug
image-debug: build-go
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:debug -o type=docker -f dist/images/Dockerfile.debug dist/images/

.PHONY: image-vpc-nat-gateway
image-vpc-nat-gateway:
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHONY: image-centos-compile
image-centos-compile:
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/centos7-compile:$(RELEASE_TAG) -o type=docker -f dist/images/compile/centos7/Dockerfile fastpath/
	# docker buildx build --platform linux/amd64 -t $(REGISTRY)/centos8-compile:$(RELEASE_TAG) -o type=docker -f dist/images/compile/centos8/Dockerfile fastpath/

.PHOONY: image-test
image-test: build-go
	docker buildx build --platform linux/amd64 -t $(REGISTRY)/test:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile.test dist/images/

.PHONY: release
release: lint image-kube-ovn image-vpc-nat-gateway image-centos-compile

.PHONY: release-arm
release-arm: build-go-arm
	docker buildx build --platform linux/arm64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build --platform linux/arm64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHONY: push-dev
push-dev:
	docker push $(REGISTRY)/kube-ovn:$(DEV_TAG)

.PHONY: push-release
push-release: release
	docker push $(REGISTRY)/kube-ovn:$(RELEASE_TAG)

.PHONY: tar-kube-ovn
tar-kube-ovn:
	docker save $(REGISTRY)/kube-ovn:$(RELEASE_TAG) $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-no-avx512 -o kube-ovn.tar

.PHONY: tar-vpc-nat-gateway
tar-vpc-nat-gateway:
	docker save $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o vpc-nat-gateway.tar

.PHONY: tar-centos-compile
tar-centos-compile:
	docker save $(REGISTRY)/centos7-compile:$(RELEASE_TAG) -o centos7-compile.tar
	# docker save $(REGISTRY)/centos8-compile:$(RELEASE_TAG) -o centos8-compile.tar

.PHONY: tar
tar: tar-kube-ovn tar-vpc-nat-gateway tar-centos-compile

.PHONY: base-tar-amd64
base-tar-amd64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64 $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-no-avx512 -o image-amd64.tar

.PHONY: base-tar-amd64-dpdk
base-tar-amd64-dpdk:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-amd64-dpdk -o image-amd64-dpdk.tar

.PHONY: base-tar-arm64
base-tar-arm64:
	docker save $(REGISTRY)/kube-ovn-base:$(RELEASE_TAG)-arm64 -o image-arm64.tar

define docker_ensure_image_exists
	if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep "^$(1)$$" >/dev/null; then \
		docker pull "$(1)"; \
	fi
endef

define docker_rm_container
	@docker ps -a -f name="$(1)" --format "{{.ID}}" | while read c; do docker rm -f $$c; done
endef

define docker_network_info
	$(eval VAR_PREFIX = $(shell echo $(1) | tr '[:lower:]' '[:upper:]'))
	$(eval $(VAR_PREFIX)_IPV4_SUBNET = $(shell docker network inspect $(1) -f "{{(index .IPAM.Config 0).Subnet}}"))
	$(eval $(VAR_PREFIX)_IPV6_SUBNET = $(shell docker network inspect $(1) -f "{{(index .IPAM.Config 1).Subnet}}"))
	$(eval $(VAR_PREFIX)_IPV4_GATEWAY = $(shell docker network inspect $(1) -f "{{(index .IPAM.Config 0).Gateway}}"))
	$(eval $(VAR_PREFIX)_IPV6_GATEWAY = $(shell docker network inspect $(1) -f "{{(index .IPAM.Config 1).Gateway}}"))
	$(eval $(VAR_PREFIX)_IPV6_GATEWAY := $(shell docker exec kube-ovn-control-plane ip -6 route show default | awk '{print $$3}'))
	$(eval $(VAR_PREFIX)_IPV4_EXCLUDE_IPS = $(shell docker network inspect $(1) -f '{{range .Containers}},{{index (split .IPv4Address "/") 0}}{{end}}' | sed 's/^,//'))
	$(eval $(VAR_PREFIX)_IPV6_EXCLUDE_IPS = $(shell docker network inspect $(1) -f '{{range .Containers}},{{index (split .IPv6Address "/") 0}}{{end}}' | sed 's/^,//'))
endef

define docker_create_vlan_network
	$(eval VLAN_NETWORK_ID = $(shell docker network ls -f name=$(E2E_NETWORK) --format '{{.ID}}'))
	@if [ ! -z "$(VLAN_ID)" -a -z "$(VLAN_NETWORK_ID)" ]; then \
		docker network create --attachable -d bridge \
			--ipv6 --subnet fc00:adb1:b29b:608d::/64 --gateway fc00:adb1:b29b:608d::1 \
			-o com.docker.network.bridge.enable_ip_masquerade=true \
			-o com.docker.network.driver.mtu=1500 $(E2E_NETWORK); \
	fi
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

define kind_create_cluster
	kind create cluster --config $(1) --name $(2)
	kubectl delete --ignore-not-found sc standard
	kubectl delete --ignore-not-found -n local-path-storage deploy local-path-provisioner
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

.PHONY: kind-generate-config
kind-generate-config:
	j2 yamls/kind.yaml.j2 -o yamls/kind.yaml

.PHONY: kind-disable-hairpin
kind-disable-hairpin:
	$(call docker_config_bridge,kind,0,)

.PHONY: kind-enable-hairpin
kind-enable-hairpin:
	$(call docker_config_bridge,kind,1,)

.PHONY: kind-create
kind-create:
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn)

.PHONY: kind-init
kind-init: kind-init-ipv4

.PHONY: kind-init-ipv4
kind-init-ipv4: kind-clean
	@$(MAKE) kind-generate-config
	@$(MAKE) kind-create

.PHONY: kind-init-ovn-ic
kind-init-ovn-ic: kind-clean-ovn-ic kind-init
	@$(MAKE) kind-generate-config
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn1)

.PHONY: kind-init-ovn-submariner
kind-init-ovn-submariner: kind-clean-ovn-submariner kind-init
	@pod_cidr_v4=10.18.0.0/16 svc_cidr_v4=10.112.0.0/12 $(MAKE) kind-generate-config
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn1)

.PHONY: kind-init-iptables
kind-init-iptables:
	@kube_proxy_mode=iptables $(MAKE) kind-init

.PHONY: kind-init-ha
kind-init-ha: kind-init-ha-ipv4

.PHONY: kind-init-ha-ipv4
kind-init-ha-ipv4:
	@ha=true $(MAKE) kind-init

.PHONY: kind-init-ha-ipv6
kind-init-ha-ipv6:
	@ip_family=ipv6 $(MAKE) kind-init-ha

.PHONY: kind-init-ha-dual
kind-init-ha-dual:
	@ip_family=dual $(MAKE) kind-init-ha

.PHONY: kind-init-single
kind-init-single:
	@single=true $(MAKE) kind-init

.PHONY: kind-init-ipv6
kind-init-ipv6:
	@ip_family=ipv6 $(MAKE) kind-init

.PHONY: kind-init-dual
kind-init-dual:
	@ip_family=dual $(MAKE) kind-init

.PHONY: kind-load-image
kind-load-image:
	$(call kind_load_image,kube-ovn,$(REGISTRY)/kube-ovn:$(VERSION))

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

.PHONY: kind-install-chart
kind-install-chart: kind-load-image kind-untaint-control-plane
	kubectl label node -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
	kubectl label node -lnode-role.kubernetes.io/control-plane kube-ovn/role=master --overwrite
	kubectl label node -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel --overwrite
	ips=$$(kubectl get node -lkube-ovn/role=master --no-headers -o wide | awk '{print $$6}') && \
	helm install kubeovn ./charts \
		--set global.images.kubeovn.tag=$(VERSION) \
		--set replicaCount=$$(echo $$ips | awk '{print NF}') \
		--set MASTER_NODES="$$(echo $$ips | tr \\n ',' | sed -e 's/,$$//' -e 's/,/\\,/g')"
	kubectl rollout status deployment/ovn-central -n kube-system --timeout 300s
	kubectl rollout status deployment/kube-ovn-controller -n kube-system --timeout 120s
	kubectl rollout status daemonset/kube-ovn-cni -n kube-system --timeout 120s
	kubectl rollout status daemonset/kube-ovn-pinger -n kube-system --timeout 120s
	kubectl rollout status deployment/coredns -n kube-system --timeout 60s

.PHONY: kind-upgrade-chart
kind-upgrade-chart: kind-load-image
	$(eval OVN_DB_IPS = $(shell kubectl get no -lkube-ovn/role=master --no-headers -o wide | awk '{print $$6}' | tr \\n ',' | sed -e 's/,$$//' -e 's/,/\\,/g'))
	helm upgrade kubeovn ./charts \
		--set global.images.kubeovn.tag=$(VERSION) \
		--set replicaCount=$$(echo $(OVN_DB_IPS) | awk -F ',' '{print NF}') \
		--set MASTER_NODES='$(OVN_DB_IPS)' \
		--set restart_ovs=$(CHART_UPGRADE_RESTART_OVS)
	kubectl rollout status deployment/ovn-central -n kube-system --timeout 300s
	kubectl rollout status daemonset/ovs-ovn -n kube-system --timeout 120s
	kubectl rollout status deployment/kube-ovn-controller -n kube-system --timeout 120s
	kubectl rollout status daemonset/kube-ovn-cni -n kube-system --timeout 120s
	kubectl rollout status daemonset/kube-ovn-pinger -n kube-system --timeout 120s

.PHONY: kind-install
kind-install: kind-load-image
	kubectl config use-context kind-kube-ovn
	@$(MAKE) kind-untaint-control-plane
	sed 's/VERSION=.*/VERSION=$(VERSION)/' dist/images/install.sh | bash
	kubectl describe no

.PHONY: kind-install-dev
kind-install-dev:
	@VERSION=$(DEV_TAG) $(MAKE) kind-install

.PHONY: kind-install-ipv4
kind-install-ipv4: kind-install-overlay-ipv4

.PHONY: kind-install-overlay-ipv4
kind-install-overlay-ipv4: kind-install

.PHONY: kind-install-ovn-ic
kind-install-ovn-ic: kind-install
	$(call kind_load_image,kube-ovn1,$(REGISTRY)/kube-ovn:$(VERSION))
	kubectl config use-context kind-kube-ovn1
	sed -e 's/10.16.0/10.18.0/g' \
		-e 's/10.96.0/10.98.0/g' \
		-e 's/100.64.0/100.68.0/g' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | bash
	kubectl describe no

	docker run -d --name ovn-ic-db --network kind $(REGISTRY)/kube-ovn:$(VERSION) bash start-ic-db.sh
	@set -e; \
	ic_db_host=$$(docker inspect ovn-ic-db -f "{{.NetworkSettings.Networks.kind.IPAddress}}"); \
	zone=az0 ic_db_host=$$ic_db_host gateway_node_name=kube-ovn-worker j2 yamls/ovn-ic.yaml.j2 -o ovn-ic-0.yaml; \
	zone=az1 ic_db_host=$$ic_db_host gateway_node_name=kube-ovn1-worker j2 yamls/ovn-ic.yaml.j2 -o ovn-ic-1.yaml
	kubectl config use-context kind-kube-ovn
	kubectl apply -f ovn-ic-0.yaml
	kubectl config use-context kind-kube-ovn1
	kubectl apply -f ovn-ic-1.yaml
	sleep 6
	docker exec ovn-ic-db ovn-ic-sbctl show

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
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		ENABLE_VLAN=true VLAN_NIC=eth0 bash
	kubectl describe no

.PHONY: kind-install-underlay-u2o-interconnection-dual
kind-install-underlay-u2o-interconnection-dual: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET),$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY),$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS),$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		ENABLE_SSL=true DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=eth0 U2O_INTERCONNECTION=true bash

.PHONY: kind-install-underlay-hairpin-ipv4
kind-install-underlay-hairpin-ipv4: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		ENABLE_VLAN=true VLAN_NIC=eth0 bash
	kubectl describe no

.PHONY: kind-install-ipv6
kind-install-ipv6: kind-install-overlay-ipv6

.PHONY: kind-install-overlay-ipv6
kind-install-overlay-ipv6:
	@IPV6=true $(MAKE) kind-install

.PHONY: kind-install-underlay-ipv6
kind-install-underlay-ipv6: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		IPV6=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-underlay-hairpin-ipv6
kind-install-underlay-hairpin-ipv6: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		IPV6=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-dual
kind-install-dual: kind-install-overlay-dual

.PHONY: kind-install-overlay-dual
kind-install-overlay-dual:
	@DUAL_STACK=true $(MAKE) kind-install

.PHONY: kind-install-underlay-dual
kind-install-underlay-dual: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET),$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY),$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS),$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-underlay-hairpin-dual
kind-install-underlay-hairpin-dual: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET),$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY),$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS),$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-underlay-logical-gateway-dual
kind-install-underlay-logical-gateway-dual: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET),$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY)9,$(KIND_IPV6_GATEWAY)f"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_GATEWAY),$(KIND_IPV4_EXCLUDE_IPS),$(KIND_IPV6_GATEWAY),$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true \
		VLAN_NIC=eth0 LOGICAL_GATEWAY=true bash

.PHONY: kind-install-multus
kind-install-multus:
	$(call kind_load_image,kube-ovn,$(MULTUS_IMAGE),1)
	kubectl apply -f "$(MULTUS_YAML)"
	kubectl -n kube-system rollout status ds kube-multus-ds

.PHONY: kind-install-vpc-nat-gw
kind-install-vpc-nat-gw: kind-load-image kind-untaint-control-plane
	$(call kind_load_image,kube-ovn,$(VPC_NAT_GW_IMG))
	@$(MAKE) ENABLE_NAT_GW=true CNI_CONFIG_PRIORITY=10 kind-install
	@$(MAKE) kind-install-multus
	kubectl apply -f yamls/vpc-nat-gw-attachment.yaml

.PHONY: kind-install-kubevirt
kind-install-kubevirt: kind-load-image kind-untaint-control-plane
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_OPERATOR_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_API_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_CONTROLLER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_HANDLER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_LAUNCHER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(KUBEVIRT_TEST_IMAGE),1)

	sed 's/VERSION=.*/VERSION=$(VERSION)/' dist/images/install.sh | bash
	kubectl describe no

	kubectl apply -f "$(KUBEVIRT_OPERATOR_YAML)"
	kubectl apply -f "$(KUBEVIRT_CR_YAML)"
	kubectl rollout status deployment/virt-operator -n kubevirt --timeout 120s
	echo "wait kubevirt releated pod running ..."
	sleep 60

	kubectl -n kubevirt patch kubevirt kubevirt --type=merge --patch '{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}}}}'
	kubectl apply -f "$(KUBEVIRT_TEST_YAML)"
	sleep 5
	kubectl patch vm testvm --type=merge --patch '{"spec":{"running":true}}'

.PHONY: kind-install-lb-svc
kind-install-lb-svc: kind-load-image kind-untaint-control-plane
	$(call kind_load_image,kube-ovn,$(VPC_NAT_GW_IMG))
	@$(MAKE) ENABLE_LB_SVC=true CNI_CONFIG_PRIORITY=10 kind-install
	@$(MAKE) kind-install-multus
	kubectl apply -f yamls/lb-svc-attachment.yaml

.PHONY: kind-install-webhook
kind-install-webhook: kind-install
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_CONTROLLER),1)
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_CAINJECTOR),1)
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_WEBHOOK),1)

	kubectl apply -f "$(CERT_MANAGER_YAML)"
	kubectl rollout status deployment/cert-manager -n cert-manager --timeout 120s
	kubectl rollout status deployment/cert-manager-cainjector -n cert-manager --timeout 120s
	kubectl rollout status deployment/cert-manager-webhook -n cert-manager --timeout 120s

	kubectl apply -f yamls/webhook.yaml
	kubectl rollout status deployment/kube-ovn-webhook -n kube-system --timeout 120s

.PHONY: kind-install-cilium-chaining
kind-install-cilium-chaining: kind-load-image kind-untaint-control-plane
	$(eval KUBERNETES_SERVICE_HOST = $(shell kubectl get nodes kube-ovn-control-plane -o jsonpath='{.status.addresses[0].address}'))
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO):v$(CILIUM_VERSION),1)
	kubectl apply -f yamls/cilium-chaining.yaml
	helm repo add cilium https://helm.cilium.io/
	helm install cilium cilium/cilium \
		--version $(CILIUM_VERSION) \
		--namespace=kube-system \
		--set k8sServiceHost=$(KUBERNETES_SERVICE_HOST) \
		--set k8sServicePort=6443 \
		--set tunnel=disabled \
		--set sessionAffinity=true \
		--set enableIPv4Masquerade=false \
		--set cni.chainingMode=generic-veth \
		--set cni.customConf=true \
		--set cni.configMap=cni-configuration
	kubectl -n kube-system rollout status ds cilium --timeout 300s
	bash dist/images/install-cilium-cli.sh
	sed 's/VERSION=.*/VERSION=$(VERSION)/' dist/images/install.sh | \
		ENABLE_LB=false ENABLE_NP=false CNI_CONFIG_PRIORITY=10 bash
	kubectl describe no

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
	trivy image --exit-code=1 --ignore-unfixed --scanners vuln $(REGISTRY)/kube-ovn:$(RELEASE_TAG)
	trivy image --exit-code=1 --ignore-unfixed --scanners vuln $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG)

.PHONY: ut
ut:
	ginkgo -mod=mod -progress --always-emit-ginkgo-writer --slow-spec-threshold=60s test/unittest
	go test ./pkg/...

.PHONY: ipam-bench
ipam-bench:
	go test -timeout 30m -bench='^BenchmarkIPAM' -benchtime=10000x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false
	go test -timeout 90m -bench='^BenchmarkParallelIPAM' -benchtime=10x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false

.PHONY: clean
clean:
	$(RM) dist/images/kube-ovn dist/images/kube-ovn-cmd
	$(RM) yamls/kind.yaml
	$(RM) ovn.yaml kube-ovn.yaml kube-ovn-crd.yaml
	$(RM) ovn-ic-0.yaml ovn-ic-1.yaml
	$(RM) kube-ovn.tar vpc-nat-gateway.tar image-amd64.tar image-arm64.tar

.PHONY: changelog
changelog:
	./hack/changelog.sh > CHANGELOG.md
