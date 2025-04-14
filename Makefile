SHELL = /bin/bash

include build.mk
include ut.mk
include kind.mk
include talos.mk
include e2e.mk

REGISTRY = kubeovn
DEV_TAG = dev
RELEASE_TAG = $(shell cat VERSION)
DEBUG_TAG = $(shell cat VERSION)-debug
LEGACY_TAG = $(shell cat VERSION)-amd64-legacy
VERSION = $(shell echo $${VERSION:-$(RELEASE_TAG)})

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

ANP_TEST_IMAGE = registry.k8s.io/e2e-test-images/agnhost:2.45
ANP_CR_YAML = https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/refs/heads/main/config/crd/experimental/policy.networking.k8s.io_adminnetworkpolicies.yaml
BANP_CR_YAML = https://raw.githubusercontent.com/kubernetes-sigs/network-policy-api/refs/heads/main/config/crd/experimental/policy.networking.k8s.io_baselineadminnetworkpolicies.yaml

define docker_ensure_image_exists
	if ! docker images --format "{{.Repository}}:{{.Tag}}" | grep "^$(1)$$" >/dev/null; then \
		docker pull "$(1)"; \
	fi
endef

define docker_rm_container
	@docker ps -a -q -f name="^$(1)$$" | while read c; do docker rm -f $$c; done
endef

define docker_network_info
	$(eval VAR_PREFIX = DOCKER_NETWORK_$(shell echo $(1) | tr '[:lower:]-' '[:upper:]_'))
	$(eval $(VAR_PREFIX)_IPV4_SUBNET = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Subnet}}{{end}}" | grep -v :))
	$(eval $(VAR_PREFIX)_IPV6_SUBNET = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Subnet}}{{end}}" | grep :))
	$(eval $(VAR_PREFIX)_IPV4_GATEWAY = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Gateway}}{{end}}" | grep -v :))
	$(eval $(VAR_PREFIX)_IPV6_GATEWAY = $(shell docker network inspect $(1) -f "{{range .IPAM.Config}}{{println .Gateway}}{{end}}" | grep :))
	$(eval $(VAR_PREFIX)_IPV6_GATEWAY := $(if $($(VAR_PREFIX)_IPV6_GATEWAY),$($(VAR_PREFIX)_IPV6_GATEWAY),$(shell docker exec $(2) ip -6 route show default | awk '{print $$3}')))
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

.PHONY: install-chart
install-chart: untaint-control-plane
	kubectl label node -lbeta.kubernetes.io/os=linux kubernetes.io/os=linux --overwrite
	kubectl label node -lnode-role.kubernetes.io/control-plane kube-ovn/role=master --overwrite
	kubectl label node -lovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel --overwrite
	helm install kubeovn ./charts/kube-ovn --wait \
		--set global.images.kubeovn.tag=$(VERSION) \
		--set OVN_DIR=$(shell echo $${OVN_DIR:-/etc/origin/ovn}) \
		--set OPENVSWITCH_DIR=$(shell echo $${OPENVSWITCH_DIR:-/etc/origin/openvswitch}) \
		--set DISABLE_MODULES_MANAGEMENT=$(shell echo $${DISABLE_MODULES_MANAGEMENT:-false}) \
		--set cni_conf.MOUNT_LOCAL_BIN_DIR=$(shell echo $${MOUNT_LOCAL_BIN_DIR:-true}) \
		--set networking.ENABLE_SSL=$(shell echo $${ENABLE_SSL:-false}) \
		--set networking.NETWORK_TYPE=$(shell echo $${NETWORK_TYPE:-geneve}) \
		--set networking.TUNNEL_TYPE=$(shell echo $${TUNNEL_TYPE:-geneve}) \
		--set networking.vlan.VLAN_INTERFACE_NAME=$(shell echo $${VLAN_INTERFACE_NAME:-}) \
		--set networking.vlan.VLAN_ID=$(shell echo $${VLAN_ID:-100}) \
		--set networking.NET_STACK=$(shell echo $${NET_STACK:-ipv4} | sed 's/^dual$$/dual_stack/') \
		--set-json networking.EXCLUDE_IPS='"$(shell echo $${EXCLUDE_IPS:-})"' \
		--set-json ipv4.POD_CIDR='"$(shell echo $${POD_CIDR:-10.16.0.0/16})"' \
		--set-json ipv4.POD_GATEWAY='"$(shell echo $${POD_GATEWAY:-10.16.0.1})"' \
		--set-json ipv6.POD_CIDR='"$(shell echo $${POD_CIDR:-fd00:10:16::/112})"' \
		--set-json ipv6.POD_GATEWAY='"$(shell echo $${POD_GATEWAY:-fd00:10:16::1})"' \
		--set-json dual_stack.POD_CIDR='"$(shell echo $${POD_CIDR:-10.16.0.0/16,fd00:10:16::/112})"' \
		--set-json dual_stack.POD_GATEWAY='"$(shell echo $${POD_GATEWAY:-10.16.0.1,fd00:10:16::1})"' \
		--set func.SECURE_SERVING=$(shell echo $${SECURE_SERVING:-false}) \
		--set func.ENABLE_BIND_LOCAL_IP=$(shell echo $${ENABLE_BIND_LOCAL_IP:-true}) \
		--set func.ENABLE_OVN_IPSEC=$(shell echo $${ENABLE_OVN_IPSEC:-false}) \
		--set func.ENABLE_IC=$(shell kubectl get node --show-labels | grep -qw "ovn.kubernetes.io/ic-gw" && echo true || echo false) \
		--set func.ENABLE_ANP=$(shell echo $${ENABLE_ANP:-false})

.PHONY: upgrade-chart
upgrade-chart:
	helm upgrade kubeovn ./charts/kube-ovn --wait \
		--set global.images.kubeovn.tag=$(VERSION) \
		--set OVN_DIR=$(shell echo $${OVN_DIR:-/etc/origin/ovn}) \
		--set OPENVSWITCH_DIR=$(shell echo $${OPENVSWITCH_DIR:-/etc/origin/openvswitch}) \
		--set DISABLE_MODULES_MANAGEMENT=$(shell echo $${DISABLE_MODULES_MANAGEMENT:-false}) \
		--set cni_conf.MOUNT_LOCAL_BIN_DIR=$(shell echo $${MOUNT_LOCAL_BIN_DIR:-true}) \
		--set networking.ENABLE_SSL=$(shell echo $${ENABLE_SSL:-false}) \
		--set networking.NETWORK_TYPE=$(shell echo $${NETWORK_TYPE:-geneve}) \
		--set networking.TUNNEL_TYPE=$(shell echo $${TUNNEL_TYPE:-geneve}) \
		--set networking.vlan.VLAN_INTERFACE_NAME=$(shell echo $${VLAN_INTERFACE_NAME:-}) \
		--set networking.vlan.VLAN_ID=$(shell echo $${VLAN_ID:-100}) \
		--set networking.NET_STACK=$(shell echo $${NET_STACK:-ipv4} | sed 's/^dual$$/dual_stack/') \
		--set-json networking.EXCLUDE_IPS='"$(shell echo $${EXCLUDE_IPS:-})"' \
		--set-json ipv4.POD_CIDR='"$(shell echo $${POD_CIDR:-10.16.0.0/16})"' \
		--set-json ipv4.POD_GATEWAY='"$(shell echo $${POD_GATEWAY:-10.16.0.1})"' \
		--set-json ipv6.POD_CIDR='"$(shell echo $${POD_CIDR:-fd00:10:16::/112})"' \
		--set-json ipv6.POD_GATEWAY='"$(shell echo $${POD_GATEWAY:-fd00:10:16::1})"' \
		--set-json dual_stack.POD_CIDR='"$(shell echo $${POD_CIDR:-10.16.0.0/16,fd00:10:16::/112})"' \
		--set-json dual_stack.POD_GATEWAY='"$(shell echo $${POD_GATEWAY:-10.16.0.1,fd00:10:16::1})"' \
		--set func.SECURE_SERVING=$(shell echo $${SECURE_SERVING:-false}) \
		--set func.ENABLE_BIND_LOCAL_IP=$(shell echo $${ENABLE_BIND_LOCAL_IP:-true}) \
		--set func.ENABLE_OVN_IPSEC=$(shell echo $${ENABLE_OVN_IPSEC:-false}) \
		--set func.ENABLE_IC=$(shell kubectl get node --show-labels | grep -qw "ovn.kubernetes.io/ic-gw" && echo true || echo false) \
		--set func.ENABLE_ANP=$(shell echo $${ENABLE_ANP:-false})
	kubectl -n kube-system wait pod --for=condition=ready -l app=ovs --timeout=60s

.PHONY: uninstall
uninstall:
	bash dist/images/cleanup.sh

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
