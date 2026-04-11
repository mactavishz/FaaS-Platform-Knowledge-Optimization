LOCAL_OPENFAAS_URL=http://127.0.0.1:8080
FAASD_SECRETES_PATH=/var/lib/faasd/secrets
LOCAL_REGISTRY_HOST=registry.local
LOCAL_REGISTRY_PORT=5050
LOCAL_REGISTRY_UI_URL=http://127.0.0.1:5080

.PHONY: faasd-login
faasd-login:
	vagrant ssh faasd -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password" | OPENFAAS_URL=$(LOCAL_OPENFAAS_URL) faas-cli login -s

.PHONY: faasd-passwd
faasd-passwd:
	vagrant ssh faasd -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password"

.PHONY: build-faasd
build-faasd: registry-up
	vagrant provision faasd --provision-with build
	make faasd-login

.PHONY: registry-up
registry-up:
	docker compose -f faasd.compose.yaml up -d

.PHONY: registry-down
registry-down:
	docker compose -f faasd.compose.yaml down

.PHONY: registry-clean
registry-clean: registry-down
	rm -rf ./registry/data/*
	make registry-up

.PHONY: configure-host-registry
configure-host-registry:
	sudo bash ./scripts/configure-host-registry.sh $(LOCAL_REGISTRY_HOST) 127.0.0.1

.PHONY: check-host-registry
check-host-registry:
	ping -c 1 $(LOCAL_REGISTRY_HOST)

.PHONY: check-faasd-registry
check-faasd-registry:
	@echo "== host: registry alias =="
	@if grep -Eq "[[:space:]]$(LOCAL_REGISTRY_HOST)([[:space:]]|$$)" /etc/hosts; then \
		echo "OK host /etc/hosts contains $(LOCAL_REGISTRY_HOST)"; \
	else \
		echo "MISSING host /etc/hosts entry for $(LOCAL_REGISTRY_HOST)"; \
		exit 1; \
	fi
	@echo "== host: registry endpoint =="
	@curl -fsS "http://127.0.0.1:$(LOCAL_REGISTRY_PORT)/v2/" >/dev/null && echo "OK registry reachable on 127.0.0.1:$(LOCAL_REGISTRY_PORT)"
	@echo "== vm: registry alias =="
	vagrant ssh faasd -c "grep -E '[[:space:]]$(LOCAL_REGISTRY_HOST)([[:space:]]|$$)' /etc/hosts >/dev/null && echo 'OK vm /etc/hosts contains $(LOCAL_REGISTRY_HOST)'"
	@echo "== vm: faasd-provider environment =="
	vagrant ssh faasd -c "sudo systemctl show faasd-provider --property=Environment --value | grep -F 'FAASD_PLAIN_HTTP_REGISTRIES=' >/dev/null && echo 'OK FAASD_PLAIN_HTTP_REGISTRIES present'"
	@echo "== vm: faasd-provider live process env =="
	vagrant ssh faasd -c 'pid=$$(sudo systemctl show faasd-provider --property=MainPID --value); sudo sh -c "tr ""\\0"" ""\\n"" </proc/$$pid/environ | grep -q ""^FAASD_PLAIN_HTTP_REGISTRIES="" && echo ""OK live process has FAASD_PLAIN_HTTP_REGISTRIES"""'
	@echo "== vm: drop-in vs service start =="
	vagrant ssh faasd -c 'echo drop-in: $$(sudo stat -c "%y" /etc/systemd/system/faasd-provider.service.d/10-local-registry.conf); echo provider started: $$(sudo systemctl show faasd-provider --property=ActiveEnterTimestamp --value)'

.PHONY: build-tinyfaas
build-tinyfaas:
	vagrant provision tinyfaas --provision-with build

.PHONY: build-tinyfaas-profile
build-tinyfaas-profile:
	@if [ -z "$(PROFILE)" ]; then echo "Usage: make build-tinyfaas-profile PROFILE=<env-file>"; exit 1; fi
	vagrant ssh tinyfaas -c "PROJECT_ROOT=/vagrant TF_ENV_FILE=/vagrant/tests/integration/env/$(PROFILE) bash /vagrant/scripts/build-tinyfaas.sh"

.PHONY: tinyfaas-up
tinyfaas-up: build-tinyfaas
	@echo "tinyFaaS local development environment is ready"
	@echo "Gateway: http://localhost:8888"

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
