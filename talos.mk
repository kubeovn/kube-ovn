# Makefile for managing Talos environment

TALOS_REGISTRY_MIRROR_NAME ?= talos-registry-mirror
TALOS_REGISTRY_MIRROR_HOST ?= 10.5.0.1
TALOS_REGISTRY_MIRROR_PORT ?= 6000
TALOS_REGISTRY_MIRROR = $(TALOS_REGISTRY_MIRROR_HOST):$(TALOS_REGISTRY_MIRROR_PORT)
TALOS_REGISTRY_MIRROR_URL = http://$(TALOS_REGISTRY_MIRROR)

TALOS_CLUSTER_NAME ?= talos

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

.PHONY: talos-init
talos-init: talos-clean talos-prepare-images
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
talos-clean:
	@echo ">>> Deleting Talos cluster..."
	@talosctl cluster destroy --name $(TALOS_CLUSTER_NAME)
	@echo ">>> Talos cluster deleted."

.PHONY: talos-install
talos-install: untaint-control-plane
	@echo ">>> Installing Kube-OVN with version $(VERSION)..."
	@echo ">>>>> Tagging Kube-OVN image..."
	@docker tag $(REGISTRY)/kube-ovn:$(VERSION) 127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/$(REGISTRY)/kube-ovn:$(VERSION)
	@echo ">>>>> Pushing Kube-OVN image..."
	@docker push 127.0.0.1:$(TALOS_REGISTRY_MIRROR_PORT)/$(REGISTRY)/kube-ovn:$(VERSION)
	@echo ">>>>> Updating node labels..."
	@kubectl label node --overwrite -l node-role.kubernetes.io/control-plane kube-ovn/role=master
	@kubectl label node --overwrite -l ovn.kubernetes.io/ovs_dp_type!=userspace ovn.kubernetes.io/ovs_dp_type=kernel
	@echo ">>>>> Installing Kube-OVN..."
	@helm install kubeovn ./charts/kube-ovn --wait \
		--set global.images.kubeovn.tag=$(VERSION) \
		--set OPENVSWITCH_DIR=/var/lib/openvswitch \
		--set OVN_DIR=/var/lib/ovn \
		--set DISABLE_MODULES_MANAGEMENT=true \
		--set networking.NET_STACK=ipv4 \
		--set networking.ENABLE_SSL=$(shell echo $${ENABLE_SSL:-false}) \
		--set func.SECURE_SERVING=$(shell echo $${SECURE_SERVING:-false}) \
		--set func.ENABLE_BIND_LOCAL_IP=$(shell echo $${ENABLE_BIND_LOCAL_IP:-true}) \
		--set func.ENABLE_ANP=$(shell echo $${ENABLE_ANP:-false}) \
		--set func.ENABLE_IC=$(shell kubectl get node --show-labels | grep -qw "ovn.kubernetes.io/ic-gw" && echo true || echo false)
