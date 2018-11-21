.PHONY: build-dev-images

.ONESHELL:
build-dev-images:
	cd dist/images
	docker build -t index.alauda.cn/alaudak8s/kube-ovn-node:dev -f Dockerfile.node .
	docker push index.alauda.cn/alaudak8s/kube-ovn-node:dev
