# Makefile for managing kind environments

UNTAINT_CONTROL_PLANE ?= true

VPC_NAT_GW_IMG = $(REGISTRY)/vpc-nat-gateway:$(VERSION)

# Cilium configuration variables (fallback if not defined in main Makefile)
CILIUM_VERSION ?= v1.18.5
CILIUM_IMAGE_REPO ?= quay.io/cilium

# renovate: datasource=docker depName=kindest/node packageName=kindest/node versioning=semver
K8S_VERSION ?= v1.35.0

KIND_NETWORK_UNDERLAY = $(shell echo $${KIND_NETWORK_UNDERLAY:-kind})
UNDERLAY_NETWORK_VAR_PREFIX = DOCKER_NETWORK_$(shell echo $(KIND_NETWORK_UNDERLAY) | tr '[:lower:]-' '[:upper:]_')
UNDERLAY_NETWORK_IPV4_SUBNET = $(UNDERLAY_NETWORK_VAR_PREFIX)_IPV4_SUBNET
UNDERLAY_NETWORK_IPV6_SUBNET = $(UNDERLAY_NETWORK_VAR_PREFIX)_IPV6_SUBNET
UNDERLAY_NETWORK_IPV4_GATEWAY = $(UNDERLAY_NETWORK_VAR_PREFIX)_IPV4_GATEWAY
UNDERLAY_NETWORK_IPV6_GATEWAY = $(UNDERLAY_NETWORK_VAR_PREFIX)_IPV6_GATEWAY
UNDERLAY_NETWORK_IPV4_EXCLUDE_IPS = $(UNDERLAY_NETWORK_VAR_PREFIX)_IPV4_EXCLUDE_IPS
UNDERLAY_NETWORK_IPV6_EXCLUDE_IPS = $(UNDERLAY_NETWORK_VAR_PREFIX)_IPV6_EXCLUDE_IPS

KIND_VLAN_NIC = $(shell echo $${KIND_VLAN_NIC:-eth0})
ifneq ($(KIND_NETWORK_UNDERLAY),kind)
KIND_VLAN_NIC = eth1
endif

KIND_AUDITING = $(shell echo $${KIND_AUDITING:-false})
ifeq ($(or $(CI),false),true)
KIND_AUDITING = true
endif

define kind_create_cluster
	kind create cluster --config $(1) --name $(2)
	@if [ "x$(3)" = "x1" ]; then \
		kubectl delete --ignore-not-found sc standard; \
		kubectl delete --ignore-not-found -n local-path-storage deploy local-path-provisioner; \
	fi
	kubectl describe no
endef

define kind_load_image
	@echo "Loading image $(2) into KIND cluster $(1)..."
	@if [ "x$(3)" = "x1" ]; then \
		$(call docker_ensure_image_exists,$(2)); \
	fi
	@if kind load docker-image --name $(1) $(2) 2>/dev/null; then \
		echo "Successfully loaded $(2) using docker-image method"; \
	else \
		echo "docker-image method failed, trying image-archive method..."; \
		if docker save $(2) | kind load image-archive --name $(1) /dev/stdin; then \
			echo "Successfully loaded $(2) using image-archive method"; \
		else \
			echo "Both methods failed. Trying direct node loading..."; \
			for node in $$(kind get nodes --name $(1)); do \
				echo "Loading $(2) into node $$node..."; \
				docker save $(2) | docker exec -i $$node ctr -n k8s.io images import -; \
			done; \
		fi; \
	fi
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

define kind_subctl_join
	kubectl config use-context kind-$(1)
	kubectl label --overwrite node $(1)-worker submariner.io/gateway=true
	subctl join broker-info-internal.subm --clusterid $(2) --clustercidr $$(echo '$(3)' | tr ';' ',') --natt=false --cable-driver vxlan --health-check=false --context=kind-$(1)
	$(call kubectl_wait_submariner_ready)
endef

define kubectl_get_control_plane_ip
	$(shell \
		options=""; \
		if [ -n "$(1)" ]; then \
			options="--context kind-$(1)"; \
		fi; \
		kubectl $${options} get nodes -l node-role.kubernetes.io/control-plane \
			-o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' \
	)
endef

.PHONY: kind-network-create-underlay
kind-network-create-underlay:
	$(eval UNDERLAY_NETWORK_ID = $(shell docker network ls -f name='^kind-underlay$$' --format '{{.ID}}'))
	@if [ -z "$(UNDERLAY_NETWORK_ID)" ]; then \
		docker network create --attachable -d bridge \
			--ipv6 --subnet fc00:19fa:9eea:6085::/64 --gateway fc00:19fa:9eea:6085::1 \
			-o com.docker.network.bridge.enable_ip_masquerade=true \
			-o com.docker.network.driver.mtu=1500 kind-underlay; \
	fi

.PHONY: kind-network-connect-underlay
kind-network-connect-underlay:
	@for node in `kind -n kube-ovn get nodes`; do \
		docker network connect kind-underlay $$node; \
		docker exec $$node ip address flush dev eth1; \
	done

.PHONY: kind-iptables-accept-underlay
kind-iptables-accept-underlay:
	$(call docker_network_info,kind,kube-ovn-control-plane)
	$(call docker_network_info,kind-underlay,kube-ovn-control-plane)
	$(call add_docker_iptables_rule,iptables,-s $(DOCKER_NETWORK_KIND_UNDERLAY_IPV4_SUBNET) -d $(DOCKER_NETWORK_KIND_IPV4_SUBNET) -j ACCEPT)
	$(call add_docker_iptables_rule,iptables,-d $(DOCKER_NETWORK_KIND_UNDERLAY_IPV4_SUBNET) -s $(DOCKER_NETWORK_KIND_IPV4_SUBNET) -j ACCEPT)
	@$(MAKE) kind-iptables-accept-underlay-ipv6

.PHONY: kind-iptables-accept-underlay-ipv6
kind-iptables-accept-underlay-ipv6:
	@if [ -n "$(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET)" ] && [ -n "$(DOCKER_NETWORK_KIND_IPV6_SUBNET)" ]; then \
		echo "Adding IPv6 iptables rules..."; \
		sudo ip6tables -t filter -C DOCKER-USER -s $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -d $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT 2>/dev/null || sudo ip6tables -t filter -I DOCKER-USER -s $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -d $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT; \
		sudo ip6tables -t raw -C PREROUTING -s $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -d $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT 2>/dev/null || sudo ip6tables -t raw -I PREROUTING -s $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -d $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT; \
		sudo ip6tables -t filter -C DOCKER-USER -d $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -s $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT 2>/dev/null || sudo ip6tables -t filter -I DOCKER-USER -d $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -s $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT; \
		sudo ip6tables -t raw -C PREROUTING -d $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -s $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT 2>/dev/null || sudo ip6tables -t raw -I PREROUTING -d $(DOCKER_NETWORK_KIND_UNDERLAY_IPV6_SUBNET) -s $(DOCKER_NETWORK_KIND_IPV6_SUBNET) -j ACCEPT; \
	else \
		echo "Skipping IPv6 iptables rules (IPv6 subnets not available)"; \
	fi

.PHONY: kind-generate-config
kind-generate-config:
	k8s_version=$(K8S_VERSION) jinjanate yamls/kind.yaml.j2 -o yamls/kind.yaml

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

.PHONY: kind-init-without-kube-proxy
kind-init-without-kube-proxy: kind-init-without-kube-proxy-ipv4

.PHONY: kind-init-without-kube-proxy-%
kind-init-without-kube-proxy-%: kind-clean
	@kube_proxy_mode=none $(MAKE) kind-init-$*

.PHONY: kind-init-cilium-chaining
kind-init-cilium-chaining: kind-init-cilium-chaining-ipv4

.PHONY: kind-init-cilium-chaining-%
kind-init-cilium-chaining-%: kind-network-create-underlay
	@kube_proxy_mode=none $(MAKE) kind-init-$*
	@$(MAKE) kind-iptables-accept-underlay
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
		-v /lib/modules:/lib/modules:ro \
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
		-v /lib/modules:/lib/modules:ro \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /var/run/netns:/var/run/netns \
		-v /var/lib/docker/containers:/var/lib/docker/containers \
		-v $(CURDIR)/yamls/clab-bgp-ha.yaml:/clab-bgp/clab.yaml \
		$(CLAB_IMAGE) clab deploy -t /clab-bgp/clab.yaml

.PHONY: kind-load-image
kind-load-image:
	$(call kind_load_image,kube-ovn,$(REGISTRY)/kube-ovn:$(VERSION))

.PHONY: kind-load-image-vpc-nat-gateway
kind-load-image-vpc-nat-gateway:
	$(call kind_load_image,kube-ovn,$(VPC_NAT_GW_IMG))

.PHONY: kind-install-chart
kind-install-chart: kind-load-image untaint-control-plane install-chart

.PHONY: kind-install-chart-ssl
kind-install-chart-ssl:
	@ENABLE_SSL=true $(MAKE) kind-install-chart

.PHONY: kind-upgrade-chart
kind-upgrade-chart: kind-load-image upgrade-chart

.PHONY: kind-install-chart-v2
kind-install-chart-v2: kind-load-image untaint-control-plane install-chart-v2

.PHONY: kind-upgrade-chart-v2
kind-upgrade-chart-v2: kind-load-image upgrade-chart-v2

.PHONY: kind-install
kind-install: kind-load-image
	kubectl config use-context kind-kube-ovn
	@if [ "$(UNTAINT_CONTROL_PLANE)" = "true" ]; then \
		$(MAKE) untaint-control-plane; \
	fi
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

.PHONY: kind-install-overlay
kind-install-overlay: kind-install-overlay-ipv4

.PHONY: kind-install-overlay-%
kind-install-overlay-%:
	@$(MAKE) kind-install-$*

.PHONY: kind-install-dev
kind-install-dev: kind-install-dev-ipv4

.PHONY: kind-install-dev-%
kind-install-dev-%:
	@VERSION=$(DEV_TAG) $(MAKE) kind-install-$*

.PHONY: kind-install-debug-%
kind-install-debug-%:
	@VERSION=$(DEBUG_TAG) $(MAKE) kind-install-$*

.PHONY: kind-install-debug-valgrind
kind-install-debug-valgrind: kind-install-debug-valgrind-ipv4

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
	@$(MAKE) untaint-control-plane
	sed -e 's/10.16.0/10.18.0/g' \
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
	@$(MAKE) untaint-control-plane
	sed -e 's/fd00:10:16:/fd00:10:18:/g' \
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
	@$(MAKE) untaint-control-plane
	sed -e 's/10.16.0/10.18.0/g' \
		-e 's/fd00:10:16:/fd00:10:18:/g' \
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
	@$(MAKE) untaint-control-plane
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

	$(call kind_subctl_join,kube-ovn,cluster0,100.64.0.0/16;10.16.0.0/16)
	$(call kind_subctl_join,kube-ovn1,cluster1,100.68.0.0/16;10.18.0.0/16)

.PHONY: kind-install-underlay
kind-install-underlay: kind-install-underlay-ipv4

.PHONY: kind-install-underlay-hairpin
kind-install-underlay-hairpin: kind-install-underlay-hairpin-ipv4

.PHONY: kind-install-underlay-ipv4
kind-install-underlay-ipv4: kind-disable-hairpin kind-load-image untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY),kube-ovn-control-plane)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_NETWORK_IPV4_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_NETWORK_IPV4_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_NETWORK_IPV4_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		ENABLE_VLAN=true VLAN_NIC=$(KIND_VLAN_NIC) bash
	kubectl describe no

.PHONY: kind-install-underlay-hairpin-ipv4
kind-install-underlay-hairpin-ipv4: kind-enable-hairpin kind-load-image untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY),kube-ovn-control-plane)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_NETWORK_IPV4_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_NETWORK_IPV4_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_NETWORK_IPV4_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		ENABLE_VLAN=true VLAN_NIC=$(KIND_VLAN_NIC) bash
	kubectl describe no

.PHONY: kind-install-underlay-ipv6
kind-install-underlay-ipv6: kind-disable-hairpin kind-load-image untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY),kube-ovn-control-plane)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_NETWORK_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_NETWORK_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_NETWORK_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		IPV6=true ENABLE_VLAN=true VLAN_NIC=$(KIND_VLAN_NIC) bash

.PHONY: kind-install-underlay-hairpin-ipv6
kind-install-underlay-hairpin-ipv6: kind-enable-hairpin kind-load-image untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY),kube-ovn-control-plane)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_NETWORK_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_NETWORK_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_NETWORK_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		IPV6=true ENABLE_VLAN=true VLAN_NIC=$(KIND_VLAN_NIC) bash

.PHONY: kind-install-underlay-dual
kind-install-underlay-dual: kind-disable-hairpin kind-load-image untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY),kube-ovn-control-plane)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_NETWORK_IPV4_SUBNET)),$($(UNDERLAY_NETWORK_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_NETWORK_IPV4_GATEWAY)),$($(UNDERLAY_NETWORK_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_NETWORK_IPV4_EXCLUDE_IPS)),$($(UNDERLAY_NETWORK_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=$(KIND_VLAN_NIC) bash

.PHONY: kind-install-underlay-hairpin-dual
kind-install-underlay-hairpin-dual: kind-enable-hairpin kind-load-image untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY),kube-ovn-control-plane)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_NETWORK_IPV4_SUBNET)),$($(UNDERLAY_NETWORK_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_NETWORK_IPV4_GATEWAY)),$($(UNDERLAY_NETWORK_IPV6_GATEWAY))"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_NETWORK_IPV4_EXCLUDE_IPS)),$($(UNDERLAY_NETWORK_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true VLAN_NIC=$(KIND_VLAN_NIC) bash

.PHONY: kind-install-underlay-u2o
kind-install-underlay-u2o: kind-install-underlay-u2o-ipv4

.PHONY: kind-install-underlay-u2o-%
kind-install-underlay-u2o-%:
	@$(MAKE) U2O_INTERCONNECTION=true kind-install-underlay-$*

.PHONY: kind-install-underlay-logical-gateway-dual
kind-install-underlay-logical-gateway-dual: kind-disable-hairpin kind-load-image untaint-control-plane
	$(call docker_network_info,$(KIND_NETWORK_UNDERLAY),kube-ovn-control-plane)
	@sed -e 's@^[[:space:]]*POD_CIDR=.*@POD_CIDR="$($(UNDERLAY_NETWORK_IPV4_SUBNET)),$($(UNDERLAY_NETWORK_IPV6_SUBNET))"@' \
		-e 's@^[[:space:]]*POD_GATEWAY=.*@POD_GATEWAY="$($(UNDERLAY_NETWORK_IPV4_GATEWAY))9,$($(UNDERLAY_NETWORK_IPV6_GATEWAY))f"@' \
		-e 's@^[[:space:]]*EXCLUDE_IPS=.*@EXCLUDE_IPS="$($(UNDERLAY_NETWORK_IPV4_GATEWAY)),$($(UNDERLAY_NETWORK_IPV4_EXCLUDE_IPS)),$($(UNDERLAY_NETWORK_IPV6_GATEWAY)),$($(UNDERLAY_NETWORK_IPV6_EXCLUDE_IPS))"@' \
		-e 's@^VLAN_ID=.*@VLAN_ID="0"@' \
		-e 's/VERSION=.*/VERSION=$(VERSION)/' \
		dist/images/install.sh | \
		DUAL_STACK=true ENABLE_VLAN=true \
		VLAN_NIC=$(KIND_VLAN_NIC) LOGICAL_GATEWAY=true bash

.PHONY: kind-install-multus
kind-install-multus:
	$(call kind_load_image,kube-ovn,$(MULTUS_IMAGE),1)
	curl -s "$(MULTUS_YAML)" | sed 's/:snapshot-thick/:$(MULTUS_VERSION)-thick/g' | kubectl apply -f -
	kubectl -n kube-system set resources ds/kube-multus-ds -c kube-multus --limits=cpu=200m,memory=200Mi
	kubectl -n kube-system rollout status ds kube-multus-ds

.PHONY: kind-install-metallb
kind-install-metallb:
	$(call docker_network_info,kind,kube-ovn-control-plane)
	$(call kind_load_image,kube-ovn,$(METALLB_CONTROLLER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(METALLB_SPEAKER_IMAGE),1)
	$(call kind_load_image,kube-ovn,$(FRR_IMAGE),1)
	helm repo add metallb $(METALLB_CHART_REPO)
	helm repo update metallb
	helm install metallb metallb/metallb --wait \
		--version $(METALLB_VERSION:v%=%) \
		--namespace metallb-system \
		--create-namespace \
		--set speaker.frr.image.tag=$(FRR_VERSION)
	$(call kubectl_wait_exist_and_ready,metallb-system,deployment,metallb-controller)
	$(call kubectl_wait_exist_and_ready,metallb-system,daemonset,metallb-speaker)

.PHONY: kind-configure-metallb
kind-configure-metallb:
	@metallb_pool=$(shell echo $(KIND_IPV4_SUBNET) | sed 's/.[^.]\+$$/.201/')-$(shell echo $(KIND_IPV4_SUBNET) | sed 's/.[^.]\+$$/.250/') \
		jinjanate yamls/metallb-cr.yaml.j2 -o metallb-cr.yaml
	kubectl apply -f metallb-cr.yaml

.PHONY: kind-install-metallb-pool-from-underlay-ipv4
kind-install-metallb-pool-from-underlay-ipv4: kind-load-image
	@$(MAKE) ENABLE_OVN_LB_PREFER_LOCAL=true LS_CT_SKIP_DST_LPORT_IPS=false kind-install
	@$(MAKE) kind-install-metallb

.PHONY: kind-install-metallb-pool-from-underlay-ipv6
kind-install-metallb-pool-from-underlay-ipv6: kind-load-image
	@$(MAKE) ENABLE_OVN_LB_PREFER_LOCAL=true LS_CT_SKIP_DST_LPORT_IPS=false IPV6=true kind-install
	@$(MAKE) IPV6=true kind-install-metallb

.PHONY: kind-install-metallb-pool-from-underlay-dual
kind-install-metallb-pool-from-underlay-dual: kind-load-image
	@$(MAKE) ENABLE_OVN_LB_PREFER_LOCAL=true LS_CT_SKIP_DST_LPORT_IPS=false DUAL_STACK=true kind-install
	@$(MAKE) DUAL_STACK=true kind-install-metallb

.PHONY: kind-install-metallb-pool-from-underlay
kind-install-metallb-pool-from-underlay: kind-install-metallb-pool-from-underlay-ipv4

.PHONY: kind-install-vpc-nat-gw
kind-install-vpc-nat-gw:
	@$(MAKE) kind-load-image-vpc-nat-gateway
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
	kubectl -n kubevirt scale deploy virt-operator --replicas=1
	$(call kubectl_wait_exist_and_ready,kubevirt,deployment,virt-operator)
	$(call kubectl_wait_exist,,crd,kubevirts.kubevirt.io)

	kubectl apply -f "$(KUBEVIRT_CR_YAML)"
	kubectl -n kubevirt patch kubevirt kubevirt --type=merge --patch \
		'{"spec":{"configuration":{"developerConfiguration":{"useEmulation":true}},"infra":{"replicas":1}}}'
	$(call kubectl_wait_exist_and_ready,kubevirt,deployment,virt-api)
	$(call kubectl_wait_exist_and_ready,kubevirt,deployment,virt-controller)
	$(call kubectl_wait_exist_and_ready,kubevirt,daemonset,virt-handler)

	kubectl -n kubevirt wait --timeout=120s --for=jsonpath='{.status.phase}'=Deployed kubevirt/kubevirt

.PHONY: kind-install-lb-svc
kind-install-lb-svc:
	@$(MAKE) kind-load-image-vpc-nat-gateway
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
	$(eval IPV4_ENABLED = $(shell if echo $* | grep -q ipv6; then echo false; else echo true; fi))
	$(eval IPV6_ENABLED = $(shell if echo $* | grep -q ipv4; then echo false; else echo true; fi))
	@echo "Installing Cilium with IPv4=$(IPV4_ENABLED) and IPv6=$(IPV6_ENABLED) ..."
	$(eval KUBERNETES_SERVICE_HOST = $(call kubectl_get_control_plane_ip))
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO)/cilium:$(CILIUM_VERSION),1)
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO)/operator-generic:$(CILIUM_VERSION),1)
	kubectl apply -f yamls/cilium-chaining.yaml
	helm repo add cilium https://helm.cilium.io/
	helm repo update cilium
	helm install cilium cilium/cilium --wait \
		--version $(CILIUM_VERSION:v%=%) \
		--namespace kube-system \
		--set k8sServiceHost=$(KUBERNETES_SERVICE_HOST) \
		--set k8sServicePort=6443 \
		--set kubeProxyReplacement=false \
		--set image.useDigest=false \
		--set operator.image.useDigest=false \
		--set operator.replicas=1 \
		--set socketLB.enabled=true \
		--set nodePort.enabled=true \
		--set externalIPs.enabled=true \
		--set hostPort.enabled=false \
		--set sessionAffinity=true \
		--set enableIPv4Masquerade=false \
		--set enableIPv6Masquerade=false \
		--set hubble.enabled=true \
		--set envoy.enabled=false \
		--set sctp.enabled=true \
		--set ipv4.enabled=$(IPV4_ENABLED) \
		--set ipv6.enabled=$(IPV6_ENABLED) \
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

.PHONY: kind-install-cilium-delegate
kind-install-cilium-delegate: kind-install-cilium-delegate-ipv4

.PHONY: kind-install-cilium-delegate-%
kind-install-cilium-delegate-%:
	$(eval IPV4_ENABLED = $(shell if echo $* | grep -q ipv6; then echo false; else echo true; fi))
	$(eval IPV6_ENABLED = $(shell if echo $* | grep -q ipv4; then echo false; else echo true; fi))
	@echo "Installing Cilium with IPv4=$(IPV4_ENABLED) and IPv6=$(IPV6_ENABLED) ..."
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO)/cilium:$(CILIUM_VERSION),1)
	$(call kind_load_image,kube-ovn,$(CILIUM_IMAGE_REPO)/operator-generic:$(CILIUM_VERSION),1)
	helm repo add cilium https://helm.cilium.io/
	helm repo update cilium
	@echo "Installing Cilium via Helm with delegate configuration..."
	helm install cilium cilium/cilium --wait \
		--namespace kube-system \
		--version $(CILIUM_VERSION:v%=%) \
		--set image.useDigest=false \
		--set operator.image.useDigest=false \
		--set cni.exclusive=false \
		--set envoy.enabled=false \
		--set ipv4.enabled=$(IPV4_ENABLED) \
		--set ipv6.enabled=$(IPV6_ENABLED)
	@echo "Waiting for Cilium to be ready..."
	kubectl -n kube-system rollout status ds cilium --timeout 120s

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

.PHONY: kind-install-node-route-bgp-eip
kind-install-node-route-bgp-eip: kind-install-vpc-nat-gw
	kubectl label node --all ovn.kubernetes.io/bgp=true --overwrite
	kubectl annotate subnet ovn-default ovn.kubernetes.io/bgp=local --overwrite
	sed -e 's#image: .*#image: $(REGISTRY)/kube-ovn:$(VERSION)#' \
		-e 's/--neighbor-address=.*/--neighbor-address=10.0.1.1/' \
		-e 's/--neighbor-as=.*/--neighbor-as=65001/' \
		-e 's/--cluster-as=.*/--cluster-as=65002/' \
		-e '/--cluster-as=/a\            - --node-route-eip-mode' yamls/speaker.yaml | \
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

.PHONY: kind-install-cert-manager
kind-install-cert-manager:
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_CONTROLLER),1)
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_CAINJECTOR),1)
	$(call kind_load_image,kube-ovn,$(CERT_MANAGER_WEBHOOK),1)

	kubectl apply -f "$(CERT_MANAGER_YAML)"

	kubectl rollout status deployment/cert-manager -n cert-manager --timeout 120s
	kubectl rollout status deployment/cert-manager-cainjector -n cert-manager --timeout 120s
	kubectl rollout status deployment/cert-manager-webhook -n cert-manager --timeout 120s

.PHONY: kind-install-ovn-ipsec-cert-manager
kind-install-ovn-ipsec-cert-manager:
	@$(MAKE) CERT_MANAGER_IPSEC_CERT=true kind-install-ovn-ipsec
	@$(MAKE) kind-install-cert-manager

	docker run --rm -v "$(CURDIR)":/etc/ovn $(REGISTRY)/kube-ovn:$(VERSION) bash generate-ssl.sh

	kubectl create secret generic -n cert-manager kube-ovn-ca --from-file=tls.key=cakey.pem --from-file=tls.crt=cacert.pem
	kubectl create secret generic -n kube-system ovn-ipsec-ca --from-file=cacert=cacert.pem
	echo '{"apiVersion": "cert-manager.io/v1", "kind": "ClusterIssuer", "metadata": {"name": "kube-ovn"}, "spec": {"ca": {"secretName": "kube-ovn-ca"}}}' | \
		kubectl apply -f -

.PHONY: kind-install-anp
kind-install-anp: kind-load-image
	$(call kind_load_image,kube-ovn,$(ANP_TEST_IMAGE),1)
	kubectl apply -f "$(ANP_CR_YAML)"
	kubectl apply -f "$(BANP_CR_YAML)"
	kubectl apply -f "$(CNP_CR_YAML)"
	@$(MAKE) ENABLE_ANP=true kind-install

.PHONY: kind-install-anp-dns-resolver
kind-install-anp-dns-resolver: kind-load-image
	$(call kind_load_image,kube-ovn,$(ANP_TEST_IMAGE),1)
	kubectl apply -f "$(ANP_CR_YAML)"
	kubectl apply -f "$(BANP_CR_YAML)"
	kubectl apply -f "$(CNP_CR_YAML)"
	@$(MAKE) ENABLE_ANP=true ENABLE_DNS_NAME_RESOLVER=true kind-install

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
		-v /lib/modules:/lib/modules:ro \
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
		-v /lib/modules:/lib/modules:ro \
		-v /var/run/docker.sock:/var/run/docker.sock \
		-v /var/run/netns:/var/run/netns \
		-v /var/lib/docker/containers:/var/lib/docker/containers \
		-v $(CURDIR)/yamls/clab-bgp-ha.yaml:/clab-bgp/clab.yaml \
		$(CLAB_IMAGE) clab destroy -t /clab-bgp/clab.yaml
	@$(MAKE) kind-clean

.PHONY: kind-ghcr-pull
kind-ghcr-pull:
	@echo $${GHCR_TOKEN} | docker login ghcr.io -u github-actions --password-stdin
	docker pull ghcr.io/kubeovn/kindest-node:$(K8S_VERSION)
	docker tag ghcr.io/kubeovn/kindest-node:$(K8S_VERSION) kindest/node:$(K8S_VERSION)

.PHONY: kind-install-multus-cilium-kubeovn-non-primary
kind-install-multus-cilium-kubeovn-non-primary: kind-install-multus-cilium-kubeovn-non-primary-ipv4

.PHONY: kind-install-multus-cilium-kubeovn-non-primary-%
kind-install-multus-cilium-kubeovn-non-primary-%:
	@echo "Setting up KIND cluster with Multus-CNI, Cilium delegate as primary CNI, and Kube-OVN as secondary CNI..."
	@echo "1. Create underlay network and connect nodes..."
	@$(MAKE) kind-network-create-underlay
	@$(MAKE) kind-network-connect-underlay
	@echo "2. Installing Cilium as primary CNI..."
	@$(MAKE) kind-install-cilium-delegate-$*
	@echo "3. Installing Multus-CNI..."
	@$(MAKE) kind-install-multus
	@echo "4. Installing Kube-OVN as secondary/non-primary CNI..."
	@$(MAKE) NET_STACK=$* ENABLE_NON_PRIMARY_CNI=true CNI_CONFIG_PRIORITY=10 kind-install-chart
	@echo "KIND cluster setup complete!"
	@echo "  - Multus: Multi-CNI support"
	@echo "  - Cilium: Primary CNI"
	@echo "  - Kube-OVN: Secondary CNI for additional network interfaces"
	@echo ""
	@echo "You can now run non-primary CNI tests with:"
	@echo "  make kube-ovn-non-primary-cni-e2e"

# v2 chart variant for non-primary CNI deployment
.PHONY: kind-install-multus-cilium-kubeovn-non-primary-v2
kind-install-multus-cilium-kubeovn-non-primary-v2: kind-install-multus-cilium-kubeovn-non-primary-v2-ipv4

.PHONY: kind-install-multus-cilium-kubeovn-non-primary-v2-%
kind-install-multus-cilium-kubeovn-non-primary-v2-%:
	@echo "Setting up KIND cluster with Multus-CNI, Cilium delegate as primary CNI, and Kube-OVN as secondary CNI..."
	@echo "1. Installing Cilium as primary CNI..."
	@$(MAKE) kind-install-cilium-delegate-$*
	@echo "2. Installing Multus-CNI..."
	@$(MAKE) kind-install-multus
	@echo "3. Installing Kube-OVN as secondary/non-primary CNI..."
	@$(MAKE) NET_STACK=$* ENABLE_NON_PRIMARY_CNI=true CNI_CONFIG_PRIORITY=10 kind-install-chart
	@echo "KIND cluster setup complete!"
	@echo "  - Multus: Multi-CNI support"
	@echo "  - Cilium: Primary CNI"
	@echo "  - Kube-OVN: Secondary CNI for additional network interfaces"
	@echo ""
	@echo "You can now run non-primary CNI tests with:"
	@echo "  make kube-ovn-non-primary-cni-e2e"

# Convenience target for the most common use case (IPv4)
.PHONY: kind-setup-non-primary-cni
kind-setup-non-primary-cni: kind-install-multus-cilium-kubeovn-non-primary

.PHONY: kind-setup-non-primary-cni-v2
kind-setup-non-primary-cni-v2: kind-install-multus-cilium-kubeovn-non-primary-v2

.PHONY: kind-install-enable-record-tunnel
kind-install-enable-record-tunnel:
	@ENABLE_RECORD_TUNNEL_KEY=true $(MAKE) kind-install