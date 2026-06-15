LOCAL_OPENFAAS_URL=http://127.0.0.1:8080
FAASD_SECRETES_PATH=/var/lib/faasd/secrets

.PHONY: faasd-login
faasd-login:
	vagrant ssh faasd -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password" | OPENFAAS_URL=$(LOCAL_OPENFAAS_URL) faas-cli login -s

.PHONY: faasd-passwd
faasd-passwd:
	@vagrant ssh faasd -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password"

.PHONY: build-faasd
build-faasd:
	vagrant suspend tinyfaas && vagrant up faasd
	vagrant provision faasd --provision-with build
	make faasd-login

.PHONY: build-tinyfaas
build-tinyfaas:
	vagrant suspend faasd && vagrant up tinyfaas
	vagrant provision tinyfaas --provision-with build

.PHONY: build-tinyfaas-profile
build-tinyfaas-profile:
	@if [ -z "$(PROFILE)" ]; then echo "Usage: make build-tinyfaas-profile PROFILE=<env-file>"; exit 1; fi
	vagrant ssh tinyfaas -c "PROJECT_ROOT=/vagrant TF_ENV_FILE=/vagrant/tests/integration/env/$(PROFILE) bash /vagrant/scripts/build-tinyfaas.sh"

.PHONY: test-tinyfaas
test-tinyfaas: build-tinyfaas
	make -C tinyFaaS unit-test
	make -C tinyFaaS integration-test

.PHONY: test-faasd
test-faasd: build-faasd
	make -C faasd unit-test
	make -C faasd integration-test

.PHONY: faasd-integration-test
faasd-integration-test:
	env -u FAASD_GATEWAY_URL go test -count=1 -v -timeout 30m ./tests/integration/faasd/...

.PHONY: tinyfaas-integration-test
tinyfaas-integration-test:
	env -u TINYFAAS_GATEWAY_URL go test -count=1 -v -timeout 30m ./tests/integration/tinyfaas/...

.PHONY: integration-test
integration-test: build-faas-cli
	make faasd-integration-test
	make tinyfaas-integration-test

.PHONY: faasd-performance-test
faasd-performance-test:
	env -u FAASD_GATEWAY_URL go test -count=1 -v -timeout 90m ./tests/performance/faasd/...

.PHONY: tinyfaas-performance-test
tinyfaas-performance-test:
	env -u TINYFAAS_GATEWAY_URL go test -count=1 -v -timeout 90m ./tests/performance/tinyfaas/...

.PHONY: performance-test
performance-test: build-faas-cli
	make faasd-performance-test
	make tinyfaas-performance-test

.PHONY: unit-test
unit-test:
	cd autoscaler && go test -v -cover -race -count=10 ./...
	cd callgraph && go test -v -cover ./...

.PHONY: build-faas-cli
build-faas-cli:
	# run make command inside faas-cli directory
	make -C faas-cli go-build

.PHONY: test-faas-cli
test-faas-cli:
	# run make command inside faas-cli directory
	make -C faas-cli test-unit

.PHONY: clean-faas-cli
clean-faas-cli:
	make -C faas-cli clean

.PHONY: clean-faasd
clean-faasd:
	make -C faasd clean

.PHONY: clean-faasd-workflow-tars
clean-faasd-workflow-tars:
	find tests/workflows/faasd -path '*/dist/*.tar' -type f -exec rm -f {} +
	
.PHONY: clean-tinyfaas
clean-tinyfaas:
	make -C tinyFaaS clean
	
.PHONY: clean
clean: clean-faasd clean-faas-cli clean-tinyfaas
	
.PHONY: clean-go-build-cache
clean-go-build-cache:
	cd faasd && go clean -cache
	cd tinyFaaS && go clean -cache
	cd faas-cli && go clean -cache
