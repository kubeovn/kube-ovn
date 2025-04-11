# Makefile for managing Talos environment

TALOS_ARCH = $(shell go env GOHOSTARCH)
TALOS_VERSION = $(shell talosctl version --client --short | awk '{print $$NF}' | tail -n 1)
TALOS_IMAGE_DIR ?= /var/lib/talos
TALOS_IMAGE_URL = https://github.com/siderolabs/talos/releases/download/$(TALOS_VERSION)/metal-$(TALOS_ARCH).iso
TALOS_IMAGE_ISO = $(TALOS_VERSION)-metal-$(TALOS_ARCH).iso
TALOS_IMAGE_PATH = $(TALOS_IMAGE_DIR)/$(TALOS_IMAGE_ISO)

TALOS_REGISTRY_MIRROR_NAME ?= talos-registry-mirror
# libvirt network gateway address
TALOS_REGISTRY_MIRROR_HOST ?= 172.99.99.1
TALOS_REGISTRY_MIRROR_PORT ?= 6000
TALOS_REGISTRY_MIRROR = $(TALOS_REGISTRY_MIRROR_HOST):$(TALOS_REGISTRY_MIRROR_PORT)
TALOS_REGISTRY_MIRROR_URL = http://$(TALOS_REGISTRY_MIRROR)

TALOS_LIBVIRT_NETWORK_NAME ?= talos
TALOS_LIBVIRT_NETWORK_XML ?= talos/libvirt-network.xml
TALOS_LIBVIRT_IMAGES_DIR ?= /var/lib/libvirt/images
TALOS_LIBVIRT_IMAGE_SIZE ?= 20G
TALOS_LIBVIRT_DOMAIN_XML_TEMPLATE ?= talos/libvirt-domain.xml.j2
TALOS_LIBVIRT_DOMAIN_XML ?= talos/libvirt-domain.xml

TALOS_CLUSTER_NAME ?= talos
TALOS_CONTROL_PLANE_NODE = $(TALOS_CLUSTER_NAME)-control-plane
TALOS_WORKER_NODE = $(TALOS_CLUSTER_NAME)-worker
TALOS_K8S_VERSION ?= 1.32.3
# DO NOT CHANGE CONTROL PLANE COUNT
TALOS_CONTROL_PLANE_COUNT = 1
TALOS_WORKER_COUNT = 1

TALOS_API_PORT ?= 50000

# geneve causes kernel panic on my local libvirt virtual machines
# use vxlan instead
TALOS_TUNNEL_TYPE = vxlan
ifeq ($(shell echo $${CI:-false}),true)
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
	@for image in $$(talosctl image default | grep -v flannel); do \
		if [ -z $$(docker images -q $$image) ]; then \
			echo ">>>> Pulling $$image..."; \
			docker pull $$image; \
		else \
			echo ">>>> Image $$image already exists."; \
		fi; \
		echo ">>>>> Tagging $$image..."; \
		img=$$(echo $$image | sed -E 's#^[^/]+/#127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/#'); \
		docker tag $$image $$img; \
		echo ">>>>> Pushing $$img to registry mirror..."; \
		docker push $$img; \
	done

.PHONY: talos-libvirt-init
talos-libvirt-init: talos-libvirt-clean
	@if [ ! -f "$(TALOS_IMAGE_PATH)" ]; then \
		sudo mkdir -p "$(TALOS_IMAGE_DIR)" && \
		sudo chmod 777 "$(TALOS_IMAGE_DIR)" && \
		echo ">>> Downloading Talos image $(TALOS_IMAGE_ISO) into $(TALOS_IMAGE_DIR)..." && \
		wget "$(TALOS_IMAGE_URL)" -O "$(TALOS_IMAGE_PATH)" && \
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
		name=$${name} index=$$i image="$(TALOS_IMAGE_PATH)" disk="$${disk}" jinjanate "$(TALOS_LIBVIRT_DOMAIN_XML_TEMPLATE)" -o "$(TALOS_LIBVIRT_DOMAIN_XML)" && \
		echo ">>> Creating libvirt domain for $${name}..." && \
		sudo virsh create --validate "$(TALOS_LIBVIRT_DOMAIN_XML)"; \
	done
	@sudo virsh list --name | grep '^$(TALOS_CLUSTER_NAME)-' | while read name; do \
		echo ">>> Waiting for interface addresses of libvirt domain $${name}..."; \
		while true; do \
			ip=$$(sudo virsh domifaddr "$${name}" | grep vnet | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
			if [ -z "$${ip}" ]; then \
				echo ">>> Waiting for IP address..."; \
				sleep 2; \
			else \
				echo ">>> IP address $${ip} found."; \
				break; \
			fi; \
		done; \
	done

.PHONY: talos-libvirt-clean
talos-libvirt-clean:
	@echo ">>> Cleaning up libvirt domains..."
	@sudo virsh list --name --all | grep '^$(TALOS_CLUSTER_NAME)-' | while read dom; do \
		sudo rm -rfv "$(TALOS_LIBVIRT_IMAGES_DIR)/$${dom}.qcow2" && \
		sudo virsh destroy $$dom; \
	done
	@echo ">>> Cleaning up libvirt network..."
	@if sudo virsh net-list --name --all | grep -q '^$(TALOS_LIBVIRT_NETWORK_NAME)$$'; then sudo virsh net-destroy $(TALOS_LIBVIRT_NETWORK_NAME); fi

.PHONY: talos-init
talos-init: talos-libvirt-init talos-prepare-images
	$(eval TALOS_CONTROL_PLANE_IP = $(shell sudo virsh domifaddr "$(TALOS_CONTROL_PLANE_NODE)" | grep vnet | awk '{print $$NF}' | awk -F/ '{print $$1}'))
	$(eval TALOS_ENDPOINT = https://$(TALOS_CONTROL_PLANE_IP):6443)
	@echo ">>> Generating Talos configuration..."
	@echo ">>> Talos endpoint: $(TALOS_ENDPOINT)"
	@echo ">>> Talos cluster name: $(TALOS_CLUSTER_NAME)"
	talosctl gen config --force --install-disk /dev/vda \
		--kubernetes-version "$(TALOS_K8S_VERSION)" \
		--registry-mirror docker.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror gcr.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror ghcr.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror registry.k8s.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--config-patch "@talos/cluster-patch.yaml" "$(TALOS_CLUSTER_NAME)" "$(TALOS_ENDPOINT)"
	mv talosconfig ~/.talos/config
	@echo ">>> Applying Talos node configuration..."
	@sudo virsh list --name | grep '^$(TALOS_CONTROL_PLANE_NODE)' | while read node; do \
		echo ">>>>>> Applying Talos control plane configuration to $${node}..."; \
		ip=$$(sudo virsh domifaddr "$${node}" | grep vnet | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
		cluster=$(TALOS_CLUSTER_NAME) node=$${node} jinjanate talos/machine-patch.yaml.j2 -o talos/machine-patch.yaml && \
		talosctl apply-config --insecure --nodes $${ip} --file controlplane.yaml --config-patch "@talos/machine-patch.yaml"; \
		echo ">>>>>> Talos control plane configuration applied to $${node}."; \
	done
	@sudo virsh list --name | grep '^$(TALOS_WORKER_NODE)' | while read node; do \
		echo ">>>>>> Applying Talos worker configuration to $${node}..."; \
		ip=$$(sudo virsh domifaddr "$${node}" | grep vnet | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
		cluster=$(TALOS_CLUSTER_NAME) node=$${node} jinjanate talos/machine-patch.yaml.j2 -o talos/machine-patch.yaml && \
		talosctl apply-config --insecure --nodes $${ip} --file worker.yaml --config-patch "@talos/machine-patch.yaml"; \
		echo ">>>>>> Talos worker configuration applied to $${node}."; \
	done
	@echo ">>> Waiting for Talos machines to be booting or running..."
	@sudo virsh list --name | grep '^$(TALOS_CLUSTER_NAME)-' | while read node; do \
		ip=$$(sudo virsh domifaddr "$${node}" | grep vnet | awk '{print $$NF}' | awk -F/ '{print $$1}'); \
		while true; do \
			stage=$$(talosctl --endpoints $${ip} --nodes $${ip} get machinestatus -o jsonpath='{.spec.stage}' 2>/dev/null); \
			if [ "$${stage}" = "booting" -o "$${stage}" = "running" ]; then \
				echo ">>> Talos machine $${node} is $${stage}."; \
				break; \
			fi; \
			echo ">>> Waiting for Talos machine $${node}..."; \
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
		echo ">>> Waiting for k8s endpoint..."; \
		sleep 2; \
	done
	@echo ">>> Waiting for all k8s nodes to be present..."
	@while true; do \
		if [ $$(kubectl get nodes -o name | wc -l) -eq $$(($(TALOS_CONTROL_PLANE_COUNT)+$(TALOS_WORKER_COUNT))) ]; then \
			echo ">>> K8s nodes are present."; \
			break; \
		fi; \
		echo ">>> Waiting for all k8s nodes to be present..."; \
		sleep 2; \
	done
	@echo ">>> Waiting for kube-proxy to be ready..."
	kubectl -n kube-system rollout status ds kube-proxy
	@echo ">>> Getting all nodes..."
	@kubectl get nodes -o wide
	@echo ">>> Getting all pods..."
	@kubectl get pods -A -o wide

.PHONY: talos-clean
talos-clean: talos-libvirt-clean
	@echo ">>> Deleting Talos registry mirror..."
	@docker rm -f $(TALOS_REGISTRY_MIRROR_NAME)
	@echo ">>> Talos registry mirror deleted."

.PHONY: talos-install-prepare
talos-install-prepare:
	$(eval IMAGE_REPO = 127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/$(REGISTRY)/kube-ovn:$(VERSION))
	@echo ">>> Installing Kube-OVN with version $(VERSION)..."
	@echo ">>>>> Tagging Kube-OVN image..."
	@docker tag "$(REGISTRY)/kube-ovn:$(VERSION)" "$(IMAGE_REPO)"
	@echo ">>>>> Pushing Kube-OVN image..."
	@docker push "$(IMAGE_REPO)"

.PHONY: talos-install-chart
talos-install-chart: talos-install-prepare
	@OVN_DIR=/var/lib/ovn \
		OPENVSWITCH_DIR=/var/lib/openvswitch \
		DISABLE_MODULES_MANAGEMENT=true \
		MOUNT_LOCAL_BIN_DIR=false \
		TUNNEL_TYPE=$(TALOS_TUNNEL_TYPE) \
		$(MAKE) install-chart

.PHONY: talos-install-chart-%
talos-install-chart-%:
	@NET_STACK=$* $(MAKE) talos-install-chart

.PHONY: talos-install
talos-install: talos-install-chart

.PHONY: talos-install-%
talos-install-%:
	@NET_STACK=$* $(MAKE) talos-install

.PHONY: talos-install-dev-%
talos-install-dev-%:
	@VERSION=$(DEV_TAG) $(MAKE) talos-install-$*

.PHONY: talos-install-dev
talos-install-dev: talos-install-dev-ipv4
