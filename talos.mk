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
	@echo ">>> Deleting Talos registry mirror..."
	@docker rm -f $(TALOS_REGISTRY_MIRROR_NAME)
	@echo ">>> Talos registry mirror deleted."

.PHONY: talos-install-prepare
talos-install-prepare: untaint-control-plane
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
