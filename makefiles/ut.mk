# Makefile for running unit tests

.PHONY: ut
ut:
	go test -coverprofile=profile.cov $$(go list ./cmd/... ./pkg/... | grep -vw '^github.com/kubeovn/kube-ovn/pkg/client')
	bash dist/images/vpcnatgateway/nat_gateway_qos_test.sh
	bash dist/images/vpcnatgateway/nat_gateway_vip_test.sh

.PHONY: ovs-sandbox
ovs-sandbox: clean-ovs-sandbox
	docker run -itd --name ut-ovs-sandbox \
		--privileged \
		-v /tmp:/tmp \
		$(REGISTRY)/kube-ovn-base:$(RELEASE_TAG) ovs-sandbox -i

.PHONY: clean-ovs-sandbox
clean-ovs-sandbox:
	file /tmp/sandbox && docker rm -f ut-ovs-sandbox && rm -fr /tmp/sandbox

.PHONY: cp-ovs-ctl
cp-ovs-ctl:
	docker cp ut-ovs-sandbox:/usr/bin/ovs-vsctl /usr/bin/ovs-vsctl
	/usr/bin/ovs-vsctl --db=unix:/tmp/sandbox/db.sock show

.PHONY: cover
cover:
	go test ./pkg/ovs ./pkg/util ./pkg/ipam -gcflags=all=-l -coverprofile=cover.out -covermode=atomic
	go tool cover -func=cover.out | grep -v "100.0%"
	go tool cover -html=cover.out -o cover.html

.PHONY: ipam-bench
ipam-bench:
	go test -timeout 30m -bench='^BenchmarkIPAM' -benchtime=10000x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false
	go test -timeout 90m -bench='^BenchmarkParallelIPAM' -benchtime=10x test/unittest/ipam_bench/ipam_test.go -args -logtostderr=false
