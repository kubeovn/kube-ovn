GO_VERSION = 1.19
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

.PHONY: build-dev
build-dev: build-go
	docker build --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:$(DEV_TAG) -f dist/images/Dockerfile dist/images/

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
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-no-avx512 -o type=docker -f dist/images/Dockerfile.no-avx512 dist/images/
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:$(RELEASE_TAG)-dpdk -o type=docker -f dist/images/Dockerfile.dpdk dist/images/

.PHONY: image-debug
image-debug: build-go
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/kube-ovn:debug -o type=docker -f dist/images/Dockerfile.debug dist/images/

.PHONY: image-vpc-nat-gateway
image-vpc-nat-gateway:
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG) -o type=docker -f dist/images/vpcnatgateway/Dockerfile dist/images/vpcnatgateway

.PHONY: image-centos-compile
image-centos-compile:
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/centos7-compile:$(RELEASE_TAG) -o type=docker -f dist/images/compile/centos7/Dockerfile fastpath/
	# docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/centos8-compile:$(RELEASE_TAG) -o type=docker -f dist/images/compile/centos8/Dockerfile fastpath/

.PHOONY: image-test
image-test: build-go
	docker buildx build --platform linux/amd64 --build-arg ARCH=amd64 -t $(REGISTRY)/test:$(RELEASE_TAG) -o type=docker -f dist/images/Dockerfile.test dist/images/

.PHONY: release
release: lint image-kube-ovn image-vpc-nat-gateway image-centos-compile

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
	@if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep "^$(1)$$" >/dev/null; then \
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
		docker run --rm --privileged --network=host $(REGISTRY)/kube-ovn:$(RELEASE_TAG) bash -ec '\
			for brif in $$(ls /sys/class/net/'$$br'/brif); do \
				echo $(2) > /sys/class/net/'$$br'/brif/$$brif/hairpin_mode; \
			done'; \
		if [ -z "$(3)" ]; then \
			docker run --rm --privileged --network=host $(REGISTRY)/kube-ovn:$(RELEASE_TAG) bash -ec '\
				echo 0 > /sys/class/net/'$$br'/bridge/vlan_filtering; \
			'; \
		else \
			docker run --rm --privileged --network=host $(REGISTRY)/kube-ovn:$(RELEASE_TAG) bash -ec '\
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
	kind load docker-image --name $(1) $(2)
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
kind-init-ovn-ic: kind-clean-ovn-ic kind-init-single
	$(call kind_create_cluster,yamls/kind.yaml,kube-ovn1)

.PHONY: kind-init-iptables
kind-init-iptables:
	@kube_proxy_mode=iptables $(MAKE) kind-init

.PHONY: kind-init-ha
kind-init-ha:
	@ha=true $(MAKE) kind-init

.PHONY: kind-init-single
kind-init-single:
	@single=true $(MAKE) kind-init

.PHONY: kind-init-ipv6
kind-init-ipv6:
	@ip_family=ipv6 $(MAKE) kind-init

.PHONY: kind-init-dual
kind-init-dual:
	@ip_family=dual $(MAKE) kind-init

.PHONY: kind-init-cilium
kind-init-cilium:
	@kube_proxy_mode=iptables $(MAKE) kind-init

.PHONY: kind-load-image
kind-load-image:
	$(call kind_load_image,kube-ovn,$(REGISTRY)/kube-ovn:$(RELEASE_TAG))

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

.PHONY: kind-helm-install
kind-helm-install: kind-untaint-control-plane
	kubectl label no -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
	kubectl label no -lnode-role.kubernetes.io/control-plane  kube-ovn/role=master --overwrite
	kubectl label no -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel  --overwrite
	$(eval MASTERNODES = $(shell docker exec -i kube-ovn-control-plane kubectl get nodes -l node-role.kubernetes.io/control-plane=""  -o jsonpath='{.items[*].status.addresses[].address}'))
	$(eval EMPTY := )
	$(eval SPACE := $(EMPTY))
	$(eval MASTERS = $(subst SPACE,,,$(strip $$(MASTERNODES))))
	helm install kubeovn ./kubeovn-helm --set MASTER_NODES=$(MASTERNODES)
	kubectl -n kube-system get pods -o wide

.PHONY: kind-install
kind-install: kind-load-image
	kubectl config use-context kind-kube-ovn
	@$(MAKE) kind-untaint-control-plane
	ENABLE_SSL=true dist/images/install.sh
	kubectl describe no

.PHONY: kind-install-dev
kind-install-dev:
	$(call kind_load_image,kube-ovn,$(REGISTRY)/kube-ovn:$(DEV_TAG))
	kubectl config use-context kind-kube-ovn
	@$(MAKE) kind-untaint-control-plane
	sed 's/VERSION=.*/VERSION=$(DEV_TAG)/' dist/images/install.sh | bash

.PHONY: kind-install-ipv4
kind-install-ipv4: kind-install-overlay-ipv4

.PHONY: kind-install-overlay-ipv4
kind-install-overlay-ipv4: kind-install

.PHONY: kind-install-ovn-ic
kind-install-ovn-ic: kind-load-image kind-install
	$(call kind_load_image,kube-ovn1,$(REGISTRY)/kube-ovn:$(RELEASE_TAG))
	kubectl config use-context kind-kube-ovn1
	sed -e 's/10.16.0/10.18.0/g' \
		-e 's/10.96.0/10.98.0/g' \
		-e 's/100.64.0/100.68.0/g' \
		dist/images/install.sh | \
		ENABLE_SSL=true bash
	kubectl describe no

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
		dist/images/install.sh | \
		ENABLE_SSL=true ENABLE_VLAN=true VLAN_NIC=eth0 bash
	kubectl describe no

.PHONY: kind-install-underlay-hairpin-ipv4
kind-install-underlay-hairpin-ipv4: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh | \
		ENABLE_SSL=true ENABLE_VLAN=true VLAN_NIC=eth0 bash
	kubectl describe no

.PHONY: kind-install-ipv6
kind-install-ipv6: kind-install-overlay-ipv6

.PHONY: kind-install-overlay-ipv6
kind-install-overlay-ipv6:
	IPV6=true $(MAKE) kind-install

.PHONY: kind-install-underlay-ipv6
kind-install-underlay-ipv6: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh | \
		ENABLE_SSL=true IPV6=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-underlay-hairpin-ipv6
kind-install-underlay-hairpin-ipv6: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh | \
		ENABLE_SSL=true IPV6=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-dual
kind-install-dual: kind-install-overlay-dual

.PHONY: kind-install-overlay-dual
kind-install-overlay-dual:
	DUAL_STACK=true $(MAKE) kind-install

.PHONY: kind-install-underlay-dual
kind-install-underlay-dual: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET),$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY),$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS),$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh | \
		ENABLE_SSL=true DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-underlay-hairpin-dual
kind-install-underlay-hairpin-dual: kind-enable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET),$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY),$(KIND_IPV6_GATEWAY)"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_EXCLUDE_IPS),$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh | \
		ENABLE_SSL=true DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=eth0 bash

.PHONY: kind-install-underlay-logical-gateway-dual
kind-install-underlay-logical-gateway-dual: kind-disable-hairpin kind-load-image kind-untaint-control-plane
	$(call docker_network_info,kind)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$(KIND_IPV4_SUBNET),$(KIND_IPV6_SUBNET)"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$(KIND_IPV4_GATEWAY)9,$(KIND_IPV6_GATEWAY)f"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$(KIND_IPV4_GATEWAY),$(KIND_IPV4_EXCLUDE_IPS),$(KIND_IPV6_GATEWAY),$(KIND_IPV6_EXCLUDE_IPS)"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		dist/images/install.sh | \
		ENABLE_SSL=true DUAL_STACK=true ENABLE_VLAN=true \
		VLAN_NIC=eth0 LOGICAL_GATEWAY=true bash

.PHONY: kind-install-multus
kind-install-multus: kind-load-image kind-untaint-control-plane
	$(call docker_ensure_image_exists,$(MULTUS_IMAGE))
	$(call kind_load_image,kube-ovn,$(MULTUS_IMAGE))
	$(call kind_load_image,kube-ovn,$(VPC_NAT_GW_IMG))
	kubectl apply -f "$(MULTUS_YAML)"
	kubectl -n kube-system rollout status ds kube-multus-ds
	kubectl apply -f yamls/lb-svc-attachment.yaml
	ENABLE_SSL=true ENABLE_LB_SVC=true CNI_CONFIG_PRIORITY=10 dist/images/install.sh
	kubectl describe no

.PHONY: kind-install-cilium
kind-install-cilium: kind-load-image kind-untaint-control-plane
	$(eval KUBERNETES_SERVICE_HOST = $(shell kubectl get nodes kube-ovn-control-plane -o jsonpath='{.status.addresses[0].address}'))
	$(call docker_ensure_image_exists,$(CILIUM_IMAGE_REPO):v$(CILIUM_VERSION))
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO):v$(CILIUM_VERSION))
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
kind-reload: kind-reload-ovs
	kubectl delete pod -n kube-system -l app=kube-ovn-controller
	kubectl delete pod -n kube-system -l app=kube-ovn-cni
	kubectl delete pod -n kube-system -l app=kube-ovn-pinger

.PHONY: kind-reload-ovs
kind-reload-ovs: kind-load-image
	kubectl delete pod -n kube-system -l app=ovs

.PHONY: kind-clean
kind-clean: kind-disable-hairpin
	$(call docker_rm_container,kube-ovn-e2e)
	kind delete cluster --name=kube-ovn

.PHONY: kind-clean-ovn-ic
kind-clean-ovn-ic: kind-clean
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
	trivy image --exit-code=1 --severity=HIGH --ignore-unfixed --security-checks vuln $(REGISTRY)/kube-ovn:$(RELEASE_TAG)
	trivy image --exit-code=1 --severity=HIGH --ignore-unfixed --security-checks vuln $(REGISTRY)/vpc-nat-gateway:$(RELEASE_TAG)

.PHONY: ut
ut:
	ginkgo -mod=mod -progress --always-emit-ginkgo-writer --slow-spec-threshold=60s test/unittest

.PHONY: e2e
e2e:
	$(call docker_create_vlan_network)
	$(eval NODE_COUNT = $(shell kind get nodes --name kube-ovn | wc -l))
	$(eval E2E_NETWORK_INFO = $(shell docker inspect -f '{{json (index .NetworkSettings.Networks "$(E2E_NETWORK)")}}' kube-ovn-control-plane))
	$(call docker_rm_container,kube-ovn-e2e)
	docker run -d --name kube-ovn-e2e --network kind --cap-add=NET_ADMIN $(REGISTRY)/kube-ovn:$(RELEASE_TAG) sleep infinity
	@if [ '$(E2E_NETWORK_INFO)' = 'null' ]; then \
		kind get nodes --name kube-ovn | while read node; do \
			docker network connect $(E2E_NETWORK) $$node; \
		done; \
	fi
	$(call docker_config_bridge,$(E2E_NETWORK),0,$(VLAN_ID))

	@echo "{" > test/e2e/network.json
	@i=0; kind get nodes --name kube-ovn | while read node; do \
		i=$$((i+1)); \
		printf '"%s": ' "$$node" >> test/e2e/network.json; \
		docker inspect -f '{{json (index .NetworkSettings.Networks "$(E2E_NETWORK)")}}' "$$node" >> test/e2e/network.json; \
		if [ $$i -ne $(NODE_COUNT) ]; then echo "," >> test/e2e/network.json; fi; \
	done
	@echo "}" >> test/e2e/network.json

	$(call docker_ensure_image_exists,kubeovn/pause:3.2)
	$(call kind_load_image,kube-ovn,kubeovn/pause:3.2)
	ginkgo -mod=mod -progress --always-emit-ginkgo-writer --slow-spec-threshold=60s test/e2e

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
	ginkgo -mod=mod -progress --always-emit-ginkgo-writer --slow-spec-threshold=60s test/e2e-underlay-single-nic

.PHONY: e2e-ovn-ic
e2e-ovn-ic:
	ginkgo -mod=mod -progress --always-emit-ginkgo-writer --slow-spec-threshold=60s test/e2e-ovn-ic

.PHONY: e2e-cilium
e2e-cilium:
	docker run -d --name kube-ovn-e2e --network kind --cap-add=NET_ADMIN $(REGISTRY)/kube-ovn:$(RELEASE_TAG) sleep infinity
	ginkgo -mod=mod -progress --always-emit-ginkgo-writer --slow-spec-threshold=60s test/e2e-cilium

.PHONY: e2e-multus
e2e-multus:
	ginkgo -mod=mod -progress --always-emit-ginkgo-writer --slow-spec-threshold=60s test/e2e-multus

.PHONY: clean
clean:
	$(RM) dist/images/kube-ovn dist/images/kube-ovn-cmd
	$(RM) yamls/kind.yaml
	$(RM) ovn.yaml kube-ovn.yaml kube-ovn-crd.yaml
	$(RM) ovn-ic-0.yaml ovn-ic-1.yaml
	$(RM) kube-ovn.tar vpc-nat-gateway.tar image-amd64.tar image-arm64.tar
	$(RM) test/e2e/ovnnb_db.* test/e2e/ovnsb_db.*
	$(RM) test/e2e/network.json test/e2e-underlay-single-nic/node/network.json

.PHONY: changelog
changelog:
	./hack/changelog.sh > CHANGELOG.md
