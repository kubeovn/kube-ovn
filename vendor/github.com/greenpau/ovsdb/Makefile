.PHONY: test ctest covdir coverage docs linter ovs qtest
APP_VERSION:=$(shell cat VERSION | head -1)
GIT_COMMIT:=$(shell git describe --dirty --always)
GIT_BRANCH:=$(shell git rev-parse --abbrev-ref HEAD -- | head -1)
BUILD_USER:=$(shell whoami)
BUILD_DATE:=$(shell date +"%Y-%m-%d")
VERBOSE:=-v
ifdef TEST
	TEST:="-run ${TEST}"
endif

all: info
	@echo "WARN: please run 'make test'"

info:
	@echo "Version: $(APP_VERSION), Branch: $(GIT_BRANCH), Revision: $(GIT_COMMIT)"
	@echo "Build on $(BUILD_DATE) by $(BUILD_USER)"

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

license:
	@addlicense -c "Paul Greenberg greenpau@outlook.com" -y 2020 *.go

release: license
	@echo "Making release"
	@go mod tidy
	@go mod verify
	@if [ $(GIT_BRANCH) != "main" ]; then echo "cannot release to non-main branch $(GIT_BRANCH)" && false; fi
	@git diff-index --quiet HEAD -- || ( echo "git directory is dirty, commit changes first" && git status && false )
	@versioned -patch
	@echo "Patched version"
	@git add VERSION
	@git commit -m "released v`cat VERSION | head -1`"
	@git tag -a v`cat VERSION | head -1` -m "v`cat VERSION | head -1`"
	@git push
	@git push --tags
	@@echo "If necessary, run the following commands:"
	@echo "  git push --delete origin v$(APP_VERSION)"
	@echo "  git tag --delete v$(APP_VERSION)"
