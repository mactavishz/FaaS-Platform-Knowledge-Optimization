LOCAL_OPENFAAS_URL=http://127.0.0.1:8080
LOCAL_TINYFAAS_URL=http://127.0.0.1:9090
LOCAL_TINYFAAS_MGMT_URL=http://127.0.0.1:9091
FAASD_SECRETES_PATH=/var/lib/faasd/secrets

.PHONY: faasd-login
faasd-login:
	vagrant ssh faasd -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password" | OPENFAAS_URL=$(LOCAL_OPENFAAS_URL) faas-cli login -s

.PHONY: faasd-passwd
faasd-passwd:
	vagrant ssh faasd -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password"

.PHONY: build-faasd
build-faasd:
	vagrant provision faasd --provision-with build

.PHONY: build-tinyfaas
build-tinyfaas:
	vagrant provision tinyfaas --provision-with build

.PHONY: build-tinyfaas-profile
build-tinyfaas-profile:
	@if [ -z "$(PROFILE)" ]; then echo "Usage: make build-tinyfaas-profile PROFILE=<env-file>"; exit 1; fi
	vagrant ssh tinyfaas -c "PROJECT_ROOT=/vagrant TF_ENV_FILE=/vagrant/tests/integration/env/$(PROFILE) bash /vagrant/scripts/build-tinyfaas.sh"

.PHONY: test-tinyfaas
test-tinyfaas:
	make -C tinyFaaS unit-test
	make -C tinyFaaS integration-test

.PHONY: test-integration
test-integration: build-faas-cli
	go test -count=1 -v -timeout 10m ./tests/integration/...

.PHONY: unit-test
unit-test:
	cd autoscaler && go test -v -cover ./...
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
