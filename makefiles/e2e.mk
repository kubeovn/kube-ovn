# Makefile for end-to-end test helpers retained in release branches

KUBECONFIG = $(shell echo $${KUBECONFIG:-$(HOME)/.kube/config})

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

.PHONY: kube-ovn-submariner-conformance-e2e
kube-ovn-submariner-conformance-e2e:
	KUBECONFIG=$(KUBECONFIG) subctl verify \
		--context kind-kube-ovn --tocontext kind-kube-ovn1 \
		--verbose --disruptive-tests

.PHONY: kube-ovn-anp-e2e
kube-ovn-anp-e2e:
	KUBECONFIG=$(KUBECONFIG) ./test/anp/conformance.sh

.PHONY: kube-ovn-cnp-e2e
kube-ovn-cnp-e2e:
	KUBECONFIG=$(KUBECONFIG) ./test/cnp/conformance.sh
