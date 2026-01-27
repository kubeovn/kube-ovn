# Makefile for running end-to-end tests

E2E_BUILD_FLAGS = -ldflags "-w -s"

KUBECONFIG = $(shell echo $${KUBECONFIG:-$(HOME)/.kube/config})

E2E_BRANCH := $(shell echo $${E2E_BRANCH:-master})
E2E_IP_FAMILY := $(shell echo $${E2E_IP_FAMILY:-ipv4})
E2E_NETWORK_MODE := $(shell echo $${E2E_NETWORK_MODE:-overlay})
E2E_CILIUM_CHAINING = $(shell echo $${E2E_CILIUM_CHAINING:-false})

K8S_CONFORMANCE_E2E_FOCUS = "sig-network.*Conformance" "sig-network.*Feature:NoSNAT"
K8S_CONFORMANCE_E2E_SKIP =
K8S_NETPOL_E2E_FOCUS = "sig-network.*Feature:NetworkPolicy"
K8S_NETPOL_E2E_SKIP = "sig-network.*NetworkPolicyLegacy"
K8S_NETPOL_LEGACY_E2E_FOCUS = "sig-network.*NetworkPolicyLegacy"

VER_MAJOR = 999
VER_MINOR = 999

ifeq ($(shell echo $(E2E_BRANCH) | grep -o ^release-),release-)
VERSION_NUM = $(subst release-,,$(E2E_BRANCH))
VER_MAJOR = $(shell echo $(VERSION_NUM) | cut -f1 -d.)
VER_MINOR = $(shell echo $(VERSION_NUM) | cut -f2 -d.)
ifeq ($(shell test $(VER_MAJOR) -lt 1 -o \( $(VER_MAJOR) -eq 1 -a $(VER_MINOR) -lt 14 \) && echo true),true)
K8S_CONFORMANCE_E2E_SKIP += "sig-network.*EndpointSlice"
endif
ifeq ($(shell test $(VER_MAJOR) -lt 1 -o \( $(VER_MAJOR) -eq 1 -a $(VER_MINOR) -lt 13 \) && echo true),true)
K8S_CONFORMANCE_E2E_SKIP += "sig-network.*ServiceCIDR and IPAddress API"
endif
ifeq ($(shell test $(VER_MAJOR) -lt 1 -o \( $(VER_MAJOR) -eq 1 -a $(VER_MINOR) -lt 12 \) && echo true),true)
K8S_CONFORMANCE_E2E_SKIP += "sig-network.*Services.*session affinity"
K8S_CONFORMANCE_E2E_SKIP += "sig-network.*Feature:SCTPConnectivity"
else
K8S_CONFORMANCE_E2E_FOCUS += "sig-network.*Networking.*Feature:SCTPConnectivity"
endif
else
K8S_CONFORMANCE_E2E_FOCUS += "sig-network.*Networking.*Feature:SCTPConnectivity"
endif

ifneq ($(E2E_IP_FAMILY),ipv6)
K8S_CONFORMANCE_E2E_FOCUS += "sig-network.*Feature:Networking-IPv4"
ifeq ($(E2E_NETWORK_MODE),overlay)
K8S_CONFORMANCE_E2E_FOCUS += "sig-network.*Feature:Networking-DNS"
endif
endif

ifeq ($(E2E_IP_FAMILY),dual)
K8S_CONFORMANCE_E2E_FOCUS += "sig-network.*Feature:IPv6DualStack"
endif

ifeq ($(E2E_CILIUM_CHAINING),true)
# https://docs.cilium.io/en/stable/configuration/sctp/
# SCTP support does not support rewriting ports for SCTP packets.
# This means that when defining services, the targetPort MUST equal the port,
# otherwise the packet will be dropped.
K8S_CONFORMANCE_E2E_SKIP += "sig-network.*Networking.*Feature:SCTPConnectivity"
ifeq ($(shell test $(VER_MAJOR) -lt 1 -o \( $(VER_MAJOR) -eq 1 -a $(VER_MINOR) -lt 14 \) && echo true),true)
# https://github.com/cilium/cilium/issues/9207
K8S_CONFORMANCE_E2E_SKIP += "sig-network.*Services.*should serve endpoints on same port and different protocols"
endif
endif

GINKGO_OUTPUT_OPT =
GINKGO_PARALLEL_OPT =
GINKGO_PARALLEL_MULTIPLIER = $(shell echo $${GINKGO_PARALLEL_MULTIPLIER:-2})
ifeq ($(or $(CI),false),true)
GINKGO_OUTPUT_OPT = --github-output --silence-skips
GINKGO_PARALLEL_OPT = --procs $$(($$(nproc) * $(GINKGO_PARALLEL_MULTIPLIER)))
endif

GINKGO_E2E_BUILD = $(GINKGO) build $(E2E_BUILD_FLAGS)
GINKGO_E2E_RUN = $(GINKGO) run $(GINKGO_OUTPUT_OPT) --randomize-all -v
GINKGO_E2E_RUN_PARALLEL = $(GINKGO_E2E_RUN) $(GINKGO_PARALLEL_OPT)

define ginkgo_option
--$(1)=$(shell echo '$(2)' | sed -E 's/^[[:space:]]+//' | sed -E 's/"[[:space:]]+"/" --$(1)="/g')
endef

TEST_BIN_ARGS = -kubeconfig $(KUBECONFIG) -num-nodes $(shell kubectl get node -o name | wc -l)

.PHONY: e2e
e2e: kube-ovn-conformance-e2e

.PHONY: e2e-build
e2e-build:
	$(GINKGO_E2E_BUILD) ./test/e2e/k8s-network
	$(GINKGO_E2E_BUILD) ./test/e2e/kube-ovn
	$(GINKGO_E2E_BUILD) ./test/e2e/ovn-ic
	$(GINKGO_E2E_BUILD) ./test/e2e/multus
	$(GINKGO_E2E_BUILD) ./test/e2e/non-primary-cni
	$(GINKGO_E2E_BUILD) ./test/e2e/lb-svc
	$(GINKGO_E2E_BUILD) ./test/e2e/vip
	$(GINKGO_E2E_BUILD) ./test/e2e/vpc-egress-gateway
	$(GINKGO_E2E_BUILD) ./test/e2e/iptables-vpc-nat-gw
	$(GINKGO_E2E_BUILD) ./test/e2e/ovn-vpc-nat-gw
	$(GINKGO_E2E_BUILD) ./test/e2e/ha
	$(GINKGO_E2E_BUILD) ./test/e2e/security
	$(GINKGO_E2E_BUILD) ./test/e2e/kubevirt
	$(GINKGO_E2E_BUILD) ./test/e2e/webhook
	$(GINKGO_E2E_BUILD) ./test/e2e/connectivity
	$(GINKGO_E2E_BUILD) ./test/e2e/metallb
	$(GINKGO_E2E_BUILD) ./test/e2e/anp-domain
	$(GINKGO_E2E_BUILD) ./test/e2e/cnp-domain

.PHONY: k8s-conformance-e2e
k8s-conformance-e2e:
	$(eval K8S_CONFORMANCE_E2E_SKIP += $(shell set -eo pipefail; \
		version=`kubectl version 2>/dev/null | grep -iw server | grep -woE 'v[0-9]+(\.[0-9]+){2}' | awk '{print} END{print "v1.29.0"}' | sort -V | head -n1`; \
		if [ $$version != "v1.29.0" -a "$(E2E_IP_FAMILY)" = "dual" ]; then \
			echo '"sig-network.*should create pod, add ipv6 and ipv4 ip to host ips.*"'; \
		fi))
	$(GINKGO_E2E_BUILD) ./test/e2e/k8s-network
	$(GINKGO_E2E_RUN_PARALLEL) --timeout=1h \
		$(call ginkgo_option,focus,$(K8S_CONFORMANCE_E2E_FOCUS)) \
		$(call ginkgo_option,skip,$(K8S_CONFORMANCE_E2E_SKIP)) \
		./test/e2e/k8s-network/k8s-network.test -- $(TEST_BIN_ARGS)

.PHONY: k8s-netpol-e2e
k8s-netpol-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/k8s-network
	$(GINKGO_E2E_RUN) --timeout=2h \
		$(call ginkgo_option,focus,$(K8S_NETPOL_E2E_FOCUS)) \
		$(call ginkgo_option,skip,$(K8S_NETPOL_E2E_SKIP)) \
		./test/e2e/k8s-network/k8s-network.test -- $(TEST_BIN_ARGS)

.PHONY: cyclonus-netpol-e2e
cyclonus-netpol-e2e:
	kubectl create ns netpol
	kubectl create clusterrolebinding cyclonus --clusterrole=cluster-admin --serviceaccount=netpol:cyclonus
	kubectl create sa cyclonus -n netpol
	kubectl create -f test/e2e/cyclonus.yaml -n netpol
	while ! kubectl wait pod --for=condition=Ready -l job-name=cyclonus -n netpol; do \
		sleep 3; \
	done
	kubectl logs -f -l job-name=cyclonus -n netpol
	kubectl -n netpol logs \
		$$(kubectl -n netpol get pod -l job-name=cyclonus -o=jsonpath={.items[0].metadata.name}) | \
		grep failed; test $$? -ne 0

.PHONY: kube-ovn-conformance-e2e
kube-ovn-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/kube-ovn
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --timeout=35m --focus=CNI:Kube-OVN ./test/e2e/kube-ovn/kube-ovn.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-ic-conformance-e2e
kube-ovn-ic-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/ovn-ic
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/ovn-ic/ovn-ic.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-submariner-conformance-e2e
kube-ovn-submariner-conformance-e2e:
	KUBECONFIG=$(KUBECONFIG) subctl verify \
		--context kind-kube-ovn --tocontext kind-kube-ovn1 \
		--verbose --disruptive-tests

.PHONY: kube-ovn-multus-conformance-e2e
kube-ovn-multus-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/multus
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --timeout=10m \
		--focus=CNI:Kube-OVN ./test/e2e/multus/multus.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-non-primary-cni-e2e
kube-ovn-non-primary-cni-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/non-primary-cni
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	TEST_CONFIG_PATH=$(shell echo $${TEST_CONFIG_PATH:-$(CURDIR)/test/e2e/non-primary-cni/testconfigs}) \
	KUBE_OVN_PRIMARY_CNI=$(shell echo $${KUBE_OVN_PRIMARY_CNI:-false}) \
	$(GINKGO_E2E_RUN_PARALLEL) --timeout=15m \
		--focus="group:non-primary-cni" ./test/e2e/non-primary-cni/non-primary-cni.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-lb-svc-conformance-e2e
kube-ovn-lb-svc-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/lb-svc
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/lb-svc/lb-svc.test -- $(TEST_BIN_ARGS)

.PHONY: vip-conformance-e2e
vip-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/vip
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/vip/vip.test -- $(TEST_BIN_ARGS)

.PHONY: vpc-egress-gateway-e2e
vpc-egress-gateway-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/vpc-egress-gateway
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	$(GINKGO_E2E_RUN_PARALLEL) --timeout=30m \
		--focus=CNI:Kube-OVN ./test/e2e/vpc-egress-gateway/vpc-egress-gateway.test -- $(TEST_BIN_ARGS)

.PHONY: iptables-eip-conformance-e2e
iptables-eip-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/iptables-vpc-nat-gw
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/iptables-vpc-nat-gw/iptables-vpc-nat-gw.test -- $(TEST_BIN_ARGS)

.PHONY: iptables-eip-qos-conformance-e2e
iptables-eip-qos-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/iptables-eip-qos
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/iptables-eip-qos/iptables-eip-qos.test -- $(TEST_BIN_ARGS)

.PHONY: iptables-vpc-nat-gw-conformance-e2e
iptables-vpc-nat-gw-conformance-e2e: iptables-eip-conformance-e2e iptables-eip-qos-conformance-e2e

.PHONY: ovn-vpc-nat-gw-conformance-e2e
ovn-vpc-nat-gw-conformance-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/ovn-vpc-nat-gw
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) \
		--focus=CNI:Kube-OVN ./test/e2e/ovn-vpc-nat-gw/ovn-vpc-nat-gw.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-ha-e2e
kube-ovn-ha-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/ha
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/ha/ha.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-security-e2e
kube-ovn-security-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/security
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/security/security.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-kubevirt-e2e
kube-ovn-kubevirt-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/kubevirt
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/kubevirt/kubevirt.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-webhook-e2e
kube-ovn-webhook-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/webhook
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/webhook/webhook.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-ipsec-e2e
kube-ovn-ipsec-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/ipsec
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN --label-filter="!cert-manager" \
		./test/e2e/ipsec/ipsec.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-ipsec-cert-mgr-e2e
kube-ovn-ipsec-cert-mgr-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/ipsec
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/ipsec/ipsec.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-anp-e2e
kube-ovn-anp-e2e:
	KUBECONFIG=$(KUBECONFIG) ./test/anp/conformance.sh

.PHONY: kube-ovn-cnp-e2e
kube-ovn-cnp-e2e:
	KUBECONFIG=$(KUBECONFIG) ./test/cnp/conformance.sh

.PHONY: kube-ovn-anp-domain-e2e
kube-ovn-anp-domain-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/anp-domain
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --timeout=30m \
		--focus=CNI:Kube-OVN ./test/e2e/anp-domain/anp-domain.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-cnp-domain-e2e
kube-ovn-cnp-domain-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/cnp-domain
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --timeout=30m \
		--focus=CNI:Kube-OVN ./test/e2e/cnp-domain/cnp-domain.test -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-connectivity-e2e
kube-ovn-connectivity-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/connectivity
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN) --procs 2 --timeout=30m \
		--focus=CNI:Kube-OVN ./test/e2e/connectivity -- $(TEST_BIN_ARGS)

.PHONY: kube-ovn-underlay-metallb-e2e
kube-ovn-underlay-metallb-e2e:
	$(GINKGO_E2E_BUILD) ./test/e2e/metallb
	E2E_BRANCH=$(E2E_BRANCH) \
	E2E_IP_FAMILY=$(E2E_IP_FAMILY) \
	E2E_NETWORK_MODE=$(E2E_NETWORK_MODE) \
	$(GINKGO_E2E_RUN_PARALLEL) --focus=CNI:Kube-OVN ./test/e2e/metallb/metallb.test -- $(TEST_BIN_ARGS)
