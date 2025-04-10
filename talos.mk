# Makefile for managing Talos environment

TALOS_ARCH = $(shell go env GOHOSTARCH)
TALOS_VERSION = $(shell talosctl version --client --short | awk '{print $$NF}' | tail -n 1)
TALOS_IMAGE_DIR ?= /var/lib/talos
TALOS_IMAGE_URL = https://github.com/siderolabs/talos/releases/download/$(TALOS_VERSION)/metal-$(TALOS_ARCH).iso
TALOS_IMAGE_ISO = $(TALOS_VERSION)-metal-$(TALOS_ARCH).iso
TALOS_IMAGE_PATH = $(TALOS_IMAGE_DIR)/$(TALOS_IMAGE_ISO)

TALOS_REGISTRY_MIRROR_NAME ?= talos-registry-mirror
TALOS_REGISTRY_MIRROR_HOST ?= 10.5.0.1
TALOS_REGISTRY_MIRROR_PORT ?= 6000
TALOS_REGISTRY_MIRROR = $(TALOS_REGISTRY_MIRROR_HOST):$(TALOS_REGISTRY_MIRROR_PORT)
TALOS_REGISTRY_MIRROR_URL = http://$(TALOS_REGISTRY_MIRROR)

TALOS_LIBVIRT_NETWORK_NAME ?= talos
TALOS_LIBVIRT_NETWORK_XML ?= talos/libvirt-network.xml
TALOS_LIBVIRT_IMAGES_DIR ?= /var/lib/libvirt/images
TALOS_LIBVIRT_IMAGE_SIZE ?= 8G
TALOS_LIBVIRT_DOMAIN_XML_TEMPLATE ?= talos/libvirt-domain.xml.j2
TALOS_LIBVIRT_DOMAIN_XML ?= talos/libvirt-domain.xml

TALOS_CLUSTER_NAME ?= talos
TALOS_K8S_VERSION ?= 1.32.3

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
	@sudo virsh net-create --validate "$(TALOS_LIBVIRT_NETWORK_XML)"
	# create libvirt domains
	@sudo mkdir -p "$(TALOS_LIBVIRT_IMAGES_DIR)"
	@sudo chmod 777 "$(TALOS_LIBVIRT_IMAGES_DIR)"
	@index=0; for name in control-plane worker; do \
		node="talos-$${name}"; \
		disk=$(TALOS_LIBVIRT_IMAGES_DIR)/$${node}.qcow2; \
		sudo rm -rf "$${disk}" && \
		echo ">>> Creating disk image for $${node}..." && \
		qemu-img create -f qcow2 "$${disk}" $(TALOS_LIBVIRT_IMAGE_SIZE) && \
		echo ">>> Generating libvirt domain xml for $${node}..." && \
		name=$${node} index=$${index} image="$(TALOS_IMAGE_PATH)" disk="$${disk}" jinjanate "$(TALOS_LIBVIRT_DOMAIN_XML_TEMPLATE)" -o "$(TALOS_LIBVIRT_DOMAIN_XML)" && \
		echo ">>> Creating libvirt domain for $${node}..." && \
		sudo virsh create --validate "$(TALOS_LIBVIRT_DOMAIN_XML)" && \
		index=$$((index + 1)); \
	done

.PHONY: talos-libvirt-clean
talos-libvirt-clean:
	@echo ">>> Cleaning up libvirt domains..."
	@sudo virsh list --name --all | grep talos- | while read dom; do sudo virsh destroy $$dom; done
	@echo ">>> Cleaning up libvirt network..."
	@if sudo virsh net-list --name --all | grep -q '^$(TALOS_LIBVIRT_NETWORK_NAME)$$'; then sudo virsh net-destroy $(TALOS_LIBVIRT_NETWORK_NAME); fi

.PHONY: talos-init
talos-init: talos-libvirt-init talos-prepare-images
	@talosctl gen config --force --install-disk /dev/vda \
		--kubernetes-version $(TALOS_K8S_VERSION) \
		--registry-mirror docker.io=http://172.99.99.1:6000 \
		--registry-mirror gcr.io=http://172.99.99.1:6000 \
		--registry-mirror ghcr.io=http://172.99.99.1:6000 \
		--registry-mirror registry.k8s.io=http://172.99.99.1:6000 \
		--config-patch @talos/cluster-patch.yaml talos https://172.99.99.223:6443

	@echo ">>> Creating Talos cluster..."
	@talosctl cluster create --name $(TALOS_CLUSTER_NAME) \
		--registry-mirror docker.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror gcr.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror ghcr.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--registry-mirror registry.k8s.io=$(TALOS_REGISTRY_MIRROR_URL) \
		--config-patch @yamls/talos-cluster-patch.yaml \
		--skip-k8s-node-readiness-check
	@echo ">>> Talos cluster created."
	@echo ">>> Downloading kubeconfig..."
	@talosctl kubeconfig -f --cluster $(TALOS_CLUSTER_NAME) -n $(TALOS_CLUSTER_NAME)-controlplane-1
	@echo ">>> Talos kubeconfig downloaded."
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
	@echo ">>> Installing Kube-OVN with version $(VERSION)..."
	@echo ">>>>> Tagging Kube-OVN image..."
	@docker tag $(REGISTRY)/kube-ovn:$(VERSION) 127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/$(REGISTRY)/kube-ovn:$(VERSION)
	@echo ">>>>> Pushing Kube-OVN image..."
	@docker push 127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/$(REGISTRY)/kube-ovn:$(VERSION)

.PHONY: talos-install-chart
talos-install-chart: talos-install-prepare
	@OVN_DIR=/var/lib/ovn \
		OPENVSWITCH_DIR=/var/lib/openvswitch \
		DISABLE_MODULES_MANAGEMENT=true \
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
