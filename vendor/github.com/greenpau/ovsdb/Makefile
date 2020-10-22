.PHONY: test ctest covdir coverage docs linter ovs qtest
VERSION:=1.0
GITCOMMIT:=$(shell git describe --dirty --always)
VERBOSE:=-v
ifdef TEST
	TEST:="-run ${TEST}"
endif

all:
	@echo "WARN: please run 'make test'"

rights:
	@sudo chmod o+rw /var/run/openvswitch/db.sock || true
	@sudo chmod o+rw /run/openvswitch/ovnnb_db.sock || true
	@sudo chmod o+rw /run/openvswitch/ovnsb_db.sock || true

linter:
	@golint
	@echo "PASS: golint"

test: covdir linter rights
	@go test $(VERBOSE) -coverprofile=.coverage/coverage.out

ctest: covdir linter rights
	@richgo version || go get -u github.com/kyoh86/richgo
	@time richgo test $(VERBOSE) "${TEST}" -coverprofile=.coverage/coverage.out

covdir:
	@mkdir -p .coverage

coverage:
	@go tool cover -html=.coverage/coverage.out -o .coverage/coverage.html

docs:
	@mkdir -p .doc
	@godoc -html github.com/greenpau/ovsdb > .doc/index.html
	@echo "Run to serve docs:"
	@echo "    godoc -goroot .doc/ -html -http \":5000\""

clean:
	@rm -rf .doc
	@rm -rf .coverage

ovs:
	@ovs-vsctl add-br vbr0 || true
	@ovs-vsctl add-port vbr0 vport0 || true
	@ovs-vsctl add-port vbr0 vport1 || true
	@sudo chmod o+rw /var/run/openvswitch/db.sock

qtest:
	@#go test -v -run TestListDatabasesMethod
	@#go test -v -run TestNewClient
	@go test -v -run TestOvsTunnelStringParse
	@go test -v -run TestOvsFlowStringParse
