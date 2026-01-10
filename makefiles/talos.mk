# Makefile for managing Talos environment

TALOS_ARCH = $(shell go env GOHOSTARCH)
TALOS_VERSION ?= $(shell talosctl version --client --short | awk '{print $$NF}' | tail -n 1)
TALOS_IMAGE_DIR ?= /var/lib/talos

# generated image download link by Talos Linux Image Factory https://factory.talos.dev/
# customization:
#   extraKernelArgs:
#     - talos.network.interface.ignore=enp0s5f1
TALOS_IMAGE_URL = https://factory.talos.dev/image/9ecea35ddd146528c1d742aab47e680a1f1137a93fc7bab55edc1afee125a658/$(TALOS_VERSION)/metal-$(TALOS_ARCH).iso
TALOS_IMAGE_ISO = $(TALOS_VERSION)-metal-$(TALOS_ARCH).iso
TALOS_IMAGE_PATH = $(TALOS_IMAGE_DIR)/$(TALOS_IMAGE_ISO)

TALOS_REGISTRY_MIRROR_NAME ?= talos-registry-mirror
# libvirt network gateway address
TALOS_REGISTRY_MIRROR_HOST_IPV4 ?= 172.99.99.1
TALOS_REGISTRY_MIRROR_HOST_IPV6 ?= 2001:db8:99:99::1
TALOS_REGISTRY_MIRROR_PORT ?= 6000
TALOS_REGISTRY_MIRROR = [$(TALOS_REGISTRY_MIRROR_HOST)]:$(TALOS_REGISTRY_MIRROR_PORT)
TALOS_REGISTRY_MIRROR_URL_IPV4 = http://$(TALOS_REGISTRY_MIRROR_HOST_IPV4):$(TALOS_REGISTRY_MIRROR_PORT)
TALOS_REGISTRY_MIRROR_URL_IPV6 = http://[$(TALOS_REGISTRY_MIRROR_HOST_IPV6)]:$(TALOS_REGISTRY_MIRROR_PORT)

TALOS_LIBVIRT_NETWORK_NAME ?= talos
TALOS_LIBVIRT_NETWORK_XML ?= talos/libvirt-network.xml
TALOS_LIBVIRT_IMAGES_DIR ?= /var/lib/libvirt/images
TALOS_LIBVIRT_IMAGE_SIZE ?= 20G
TALOS_LIBVIRT_DOMAIN_XML_TEMPLATE ?= talos/libvirt-domain.xml.j2
TALOS_LIBVIRT_DOMAIN_XML ?= talos/libvirt-domain.xml

TALOS_CLUSTER_NAME ?= talos
TALOS_CONTROL_PLANE_NODE = $(TALOS_CLUSTER_NAME)-control-plane
TALOS_CONTROL_PLANE_IPV4 = 172.99.99.10
TALOS_CONTROL_PLANE_IPV6 = 2001:db8:99:99::10
TALOS_WORKER_NODE = $(TALOS_CLUSTER_NAME)-worker
TALOS_K8S_VERSION ?= 1.35.0
# DO NOT CHANGE CONTROL PLANE COUNT
TALOS_CONTROL_PLANE_COUNT = 1
TALOS_WORKER_COUNT ?= 1

TALOS_API_PORT ?= 50000

TALOS_UNDERLAY_CIDR_IPV4 = 172.99.99.0/24
TALOS_UNDERLAY_CIDR_IPV6 = 2001:db8:99:99::1/120
TALOS_UNDERLAY_CIDR_DUAL = $(TALOS_UNDERLAY_CIDR_IPV4),$(TALOS_UNDERLAY_CIDR_IPV6)
TALOS_UNDERLAY_GATEWAY_IPV4 = 172.99.99.1
TALOS_UNDERLAY_GATEWAY_IPV6 = 2001:db8:99:99::1
TALOS_UNDERLAY_GATEWAY_DUAL = $(TALOS_UNDERLAY_GATEWAY_IPV4),$(TALOS_UNDERLAY_GATEWAY_IPV6)
TALOS_UNDERLAY_EXCLUDE_IPS_IPV4 = 172.99.99.11..172.99.99.99
TALOS_UNDERLAY_EXCLUDE_IPS_IPV6 = 2001:db8:99:99::11..2001:db8:99:99::99
TALOS_UNDERLAY_EXCLUDE_IPS_DUAL = $(TALOS_UNDERLAY_EXCLUDE_IPS_IPV4),$(TALOS_UNDERLAY_EXCLUDE_IPS_IPV6)

# geneve causes kernel panic on my local libvirt virtual machines
# use vxlan instead
TALOS_TUNNEL_TYPE = vxlan
ifeq ($(or $(CI),false),true)
TALOS_TUNNEL_TYPE = geneve
endif

.PHONY: talos-registry-mirror
talos-registry-mirror:
	@if [ -z $$(docker ps -a -q -f name="^$(TALOS_REGISTRY_MIRROR_NAME)$$") ]; then \
		echo ">>> Creating Talos registry mirror..."; \
		docker run -d -p $(TALOS_REGISTRY_MIRROR_PORT):5000 --restart=always --name $(TALOS_REGISTRY_MIRROR_NAME) registry:2; \
		echo ">>> Talos registry mirror created."; \
	else \
		echo ">>> Talos registry mirror already exists."; \
	fi

.PHONY: talos-prepare-images
talos-prepare-images: talos-registry-mirror
	@echo ">>> Preparing Talos images..."
	@for image in ghcr.io/siderolabs/installer:$(TALOS_VERSION) $$(talosctl image default | grep -v flannel); do \
		if echo "$$image" | grep -q kube; then \
			image=$$(echo $$image | sed -e 's/:v\([[:digit:]]\+\.\)\{2\}[[:digit:]]\+$$/:v$(TALOS_K8S_VERSION)/'); \
		fi; \
		if [ -z $$(docker images -q $$image) ]; then \
			echo ">>> Pulling $$image..."; \
			docker pull $$image || exit 1; \
		else \
			echo ">>> Image $$image already exists."; \
		fi; \
		echo ">>>>>> Tagging $$image..."; \
		img=$$(echo $$image | sed -E 's#^[^/]+/#127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/#'); \
		docker tag $$image $$img; \
		echo ">>>>>> Pushing $$img to registry mirror..."; \
		docker push --quiet $$img || exit 1; \
	done

.PHONY: talos-libvirt-init
talos-libvirt-init: talos-libvirt-clean
	@if [ ! -f "$(TALOS_IMAGE_PATH)" ]; then \
		sudo mkdir -p "$(TALOS_IMAGE_DIR)" && \
		sudo chmod 777 "$(TALOS_IMAGE_DIR)" && \
		echo ">>> Downloading Talos image $(TALOS_IMAGE_URL) into $(TALOS_IMAGE_DIR)..." && \
		wget "$(TALOS_IMAGE_URL)" --quiet -O "$(TALOS_IMAGE_PATH)" && \
		echo ">>> Talos image downloaded."; \
	fi
	@echo ">>> Creating libvirt network $(TALOS_LIBVIRT_NETWORK_NAME)..."
	sudo virsh net-create --validate "$(TALOS_LIBVIRT_NETWORK_XML)"
	# create libvirt domains
	sudo mkdir -p "$(TALOS_LIBVIRT_IMAGES_DIR)"
	sudo chmod 777 "$(TALOS_LIBVIRT_IMAGES_DIR)"
	@for ((i=1; i<=$(TALOS_CONTROL_PLANE_COUNT)+$(TALOS_WORKER_COUNT); i++)); do \
		if [ $$i -le $(TALOS_CONTROL_PLANE_COUNT) ]; then \
			name="$(TALOS_CONTROL_PLANE_NODE)"; \
			if [ $(TALOS_CONTROL_PLANE_COUNT) -ne 1 ]; then \
				name="$(TALOS_CONTROL_PLANE_NODE)-$${i}"; \
			fi; \
		else \
			name="$(TALOS_WORKER_NODE)"; \
			if [ $(TALOS_WORKER_COUNT) -ne 1 ]; then \
				name="$(TALOS_WORKER_NODE)-$$((i-$(TALOS_CONTROL_PLANE_COUNT)))"; \
			fi; \
		fi; \
		disk=$(TALOS_LIBVIRT_IMAGES_DIR)/$${name}.qcow2; \
		sudo rm -rf "$${disk}" && \
		echo ">>> Creating disk image for $${name}..." && \
		qemu-img create -f qcow2 "$${disk}" $(TALOS_LIBVIRT_IMAGE_SIZE) && \
		echo ">>> Generating libvirt domain xml for $${name}..." && \
		name=$${name} index=$$i image="$(TALOS_IMAGE_PATH)" disk="$${disk}" \
			jinjanate "$(TALOS_LIBVIRT_DOMAIN_XML_TEMPLATE)" -o "$(TALOS_LIBVIRT_DOMAIN_XML)" && \
		echo ">>> Creating libvirt domain for $${name}..." && \
		sudo virsh create --validate "$(TALOS_LIBVIRT_DOMAIN_XML)"; \
	done
	@$(MAKE) talos-libvirt-wait-address

.PHONY: talos-libvirt-wait-address-%
talos-libvirt-wait-address-%:
	@sudo virsh list --name | grep '^$(TALOS_CLUSTER_NAME)-' | while read name; do \
		echo ">>> Waiting for interface addresses of libvirt domain $${name}..."; \
		while true; do \
			ip=$$(sudo virsh domifaddr --full "$${name}" | grep -w vnet0 | grep -iw $* | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
			if [ -z "$${ip}" ]; then \
				echo ">>> Waiting for $* address..."; \
				sleep 2; \
			else \
				echo ">>> IP address $${ip} found."; \
				break; \
			fi; \
		done; \
	done

.PHONY: talos-libvirt-wait-address
talos-libvirt-wait-address: talos-libvirt-wait-address-ipv4

.PHONY: talos-libvirt-clean
talos-libvirt-clean:
	@echo ">>> Cleaning up libvirt domains..."
	@sudo virsh list --name --all | grep '^$(TALOS_CLUSTER_NAME)-' | while read dom; do \
		sudo virsh destroy $$dom && \
		sudo rm -rfv "$(TALOS_LIBVIRT_IMAGES_DIR)/$${dom}.qcow2"; \
	done
	@echo ">>> Cleaning up libvirt network..."
	@if sudo virsh net-list --name --all | grep -q '^$(TALOS_LIBVIRT_NETWORK_NAME)$$'; then \
		sudo virsh net-destroy $(TALOS_LIBVIRT_NETWORK_NAME); \
	fi

.PHONY: talos-apply-config-%
talos-apply-config-%:
	$(eval TALOS_ENDPOINT_IP_FAMILY = $(shell echo $* | sed 's/dual/ipv4/'))
	$(eval TALOS_CONTROL_PLANE_IP = $(TALOS_CONTROL_PLANE_$(shell echo $(TALOS_ENDPOINT_IP_FAMILY) | tr '[:lower:]' '[:upper:]')))
	$(eval TALOS_ENDPOINT = https://$(if $(filter ipv6,$(TALOS_ENDPOINT_IP_FAMILY)),[$(TALOS_CONTROL_PLANE_IP)],$(TALOS_CONTROL_PLANE_IP)):6443)
	$(eval TALOS_REGISTRY_MIRROR_URL = $(TALOS_REGISTRY_MIRROR_URL_$(shell echo $(TALOS_ENDPOINT_IP_FAMILY) | tr '[:lower:]' '[:upper:]')))
	@echo ">>> Generating Talos configuration..."
	ip_family=$* jinjanate talos/cluster-config.yaml.j2 -o talos/cluster-config.yaml
	talosctl gen config --force -o talos \
		--kubernetes-version "$(TALOS_K8S_VERSION)" \
		--registry-mirror docker.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror gcr.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror ghcr.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror registry.k8s.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--config-patch "@talos/cluster-config.yaml" "$(TALOS_CLUSTER_NAME)" "$(TALOS_ENDPOINT)"
	mv talos/talosconfig ~/.talos/config
	@echo ">>> Applying Talos node $* configuration..."
	@sudo virsh list --name | grep '^$(TALOS_CONTROL_PLANE_NODE)' | while read node; do \
		echo ">>>>>> Applying Talos control plane configuration to $${node}..."; \
		ip=$$(sudo virsh domifaddr --full "$${node}" | grep -w vnet0 | grep -iw ipv4 | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
		ip_family=$* cluster=$(TALOS_CLUSTER_NAME) node=$${node} jinjanate talos/machine-config.yaml.j2 -o talos/machine-config.yaml && \
		talosctl apply-config --insecure --nodes $${ip} --file talos/controlplane.yaml --config-patch "@talos/machine-config.yaml" || exit 1; \
		echo ">>>>>> Talos control plane configuration applied to $${node}."; \
	done
	@sudo virsh list --name | grep '^$(TALOS_WORKER_NODE)' | while read node; do \
		echo ">>>>>> Applying Talos worker configuration to $${node}..."; \
		ip=$$(sudo virsh domifaddr --full "$${node}" | grep -w vnet0 | grep -iw ipv4 | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
		ip_family=$* cluster=$(TALOS_CLUSTER_NAME) node=$${node} jinjanate talos/machine-config.yaml.j2 -o talos/machine-config.yaml && \
		talosctl apply-config --insecure --nodes $${ip} --file talos/worker.yaml --config-patch "@talos/machine-config.yaml" || exit 1; \
		echo ">>>>>> Talos worker configuration applied to $${node}."; \
	done
	@$(MAKE) talos-libvirt-wait-address-$(TALOS_ENDPOINT_IP_FAMILY)

.PHONY: talos-init-%
talos-init-%: talos-libvirt-init talos-prepare-images talos-apply-config-%
	$(eval TALOS_ENDPOINT_IP_FAMILY = $(shell echo $* | sed 's/dual/ipv4/'))
	$(eval TALOS_CONTROL_PLANE_IP = $(TALOS_CONTROL_PLANE_$(shell echo $(TALOS_ENDPOINT_IP_FAMILY) | tr '[:lower:]' '[:upper:]')))
	@echo ">>> Waiting for Talos machines to be ready for bootstrapping..."
	@sudo virsh list --name | grep '^$(TALOS_CLUSTER_NAME)-' | while read node; do \
		ip=$$(sudo virsh domifaddr --full "$${node}" | grep -w vnet0 | grep -iw $(TALOS_ENDPOINT_IP_FAMILY) | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
		echo ">>>>>> Machine $${node} has an ip address $${ip}."; \
		while true; do \
			stage=$$(talosctl --endpoints $${ip} --nodes $${ip} get machinestatus -o jsonpath='{.spec.stage}' 2>/dev/null); \
			if [ $$? -ne 0 ]; then \
				echo ">>>>>> Talos api on machine $${node} is not reachable..."; \
				sleep 2; \
				continue; \
			fi; \
			if [ "$${stage}" = "running" ]; then \
				echo ">>>>>> Talos machine $${node} is $${stage}."; \
				break; \
			fi; \
			if echo "$${node}" | grep -q '^$(TALOS_WORKER_NODE)'; then \
				echo ">>>>>> Machine stage of $${node} is $${stage}, waiting for it to be running..."; \
				sleep 2; \
				continue; \
			fi; \
			if [ "$${stage}" != "booting" ]; then \
				echo ">>>>>> Machine stage of $${node} is $${stage}, waiting for it to be booting..."; \
				sleep 2; \
				continue; \
			fi; \
			status=$$(talosctl --endpoints $${ip} --nodes $${ip} service etcd status | grep -iw '^STATE' | awk '{print $$NF}' | tr 'A-Z' 'a-z'); \
			if [ "$${status}" = "preparing" ]; then \
				echo ">>>>>> Talos machine $${node} etcd is $${status}."; \
				break; \
			fi; \
			echo ">>>>>> Talos machine $${node} etcd is $${status}, waiting for it to be preparing..."; \
			sleep 2; \
		done; \
	done
	@echo ">>> Bootstrapping Talos cluster..."
	talosctl bootstrap --nodes "$(TALOS_CONTROL_PLANE_IP)" --endpoints "$(TALOS_CONTROL_PLANE_IP)"
	@echo ">>> Talos cluster bootstrapped."
	@echo ">>> Downloading Talos cluster kubeconfig..."
	talosctl kubeconfig --force --nodes "$(TALOS_CONTROL_PLANE_IP)" --endpoints "$(TALOS_CONTROL_PLANE_IP)"
	@echo ">>> Talos cluster kubeconfig downloaded."
	@echo ">>> Waiting for k8s endpoint to be ready..."
	@while true; do \
		if kubectl get nodes &>/dev/null; then \
			echo ">>> K8s endpoint is ready."; \
			break; \
		fi; \
		echo ">>>>>> Waiting for k8s endpoint..."; \
		sleep 2; \
	done
	@echo ">>> Waiting for all k8s nodes to be registered..."
	@while true; do \
		count=$$(kubectl get nodes -o name | wc -l); \
		echo ">>>>>> $${count} node(s) are registered..."; \
		if [ $${count} -eq $$(($(TALOS_CONTROL_PLANE_COUNT)+$(TALOS_WORKER_COUNT))) ]; then \
			echo ">>> All k8s nodes are registered."; \
			break; \
		fi; \
		sleep 2; \
	done
	@echo ">>> Waiting for kube-proxy to be ready..."
	kubectl -n kube-system rollout status ds kube-proxy
	@echo ">>> Getting all nodes..."
	@kubectl get nodes -o wide
	@echo ">>> Getting all pods..."
	@kubectl get pods -A -o wide

.PHONY: talos-init
talos-init: talos-init-ipv4

.PHONY: talos-init-single
talos-init-single:
	@TALOS_WORKER_COUNT=0 $(MAKE) talos-init

.PHONY: talos-clean
talos-clean: talos-libvirt-clean
	@echo ">>> Deleting Talos registry mirror..."
	@docker rm -f $(TALOS_REGISTRY_MIRROR_NAME)
	@echo ">>> Talos registry mirror deleted."

.PHONY: talos-install-prepare
talos-install-prepare:
	$(eval IMAGE_REPO = 127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/$(REGISTRY)/kube-ovn:$(VERSION))
	@echo ">>> Installing Kube-OVN with version $(VERSION)..."
	@echo ">>>>>> Tagging Kube-OVN image..."
	@docker tag "$(REGISTRY)/kube-ovn:$(VERSION)" "$(IMAGE_REPO)"
	@echo ">>>>>> Pushing Kube-OVN image..."
	@docker push --quiet "$(IMAGE_REPO)"

.PHONY: talos-install
talos-install: talos-install-prepare
	@OVN_DIR=/var/lib/ovn \
		OPENVSWITCH_DIR=/var/lib/openvswitch \
		DISABLE_MODULES_MANAGEMENT=true \
		MOUNT_LOCAL_BIN_DIR=false \
		ENABLE_TPROXY=true \
		IMAGE_PULL_POLICY=Always \
		TUNNEL_TYPE=$(TALOS_TUNNEL_TYPE) \
		$(MAKE) install-chart

.PHONY: talos-install-%
talos-install-%: talos-install-overlay-%

.PHONY: talos-install-ipv4
talos-install-ipv4: talos-install-overlay-ipv4

.PHONY: talos-install-ipv6
talos-install-ipv6: talos-install-overlay-ipv6

.PHONY: talos-install-dual
talos-install-dual: talos-install-overlay-dual

.PHONY: talos-install-dev
talos-install-dev: talos-install-dev-ipv4

.PHONY: talos-install-dev-%
talos-install-dev-%:
	@VERSION=$(DEV_TAG) $(MAKE) talos-install-$*

.PHONY: talos-install-overlay
talos-install-overlay: talos-install-overlay-ipv4

.PHONY: talos-install-overlay-%
talos-install-overlay-%:
	@NET_STACK=$* $(MAKE) talos-install

.PHONY: talos-install-underlay
talos-install-underlay: talos-install-underlay-ipv4

.PHONY: talos-install-underlay-%
talos-install-underlay-%:
	@$(eval UNDERLAY_VAR_SUFFIX = $(shell echo $* | tr 'a-z' 'A-Z'))
	@NET_STACK=$* NETWORK_TYPE=vlan VLAN_INTERFACE_NAME=enp0s5f1 VLAN_ID=0 \
		POD_CIDR=$(TALOS_UNDERLAY_CIDR_$(UNDERLAY_VAR_SUFFIX)) \
		POD_GATEWAY=$(TALOS_UNDERLAY_GATEWAY_$(UNDERLAY_VAR_SUFFIX)) \
		EXCLUDE_IPS=$(TALOS_UNDERLAY_EXCLUDE_IPS_$(UNDERLAY_VAR_SUFFIX)) \
		$(MAKE) talos-install
