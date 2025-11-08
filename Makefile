LOCAL_OPENFAAS_URL=http://127.0.0.1:8080
FAASD_SECRETES_PATH=/var/lib/faasd/secrets

.PHONY: login
login:
	vagrant ssh -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password" | OPENFAAS_URL=$(LOCAL_OPENFAAS_URL) faas-cli login -s

.PHONY: show-passwd
show-passwd:
	vagrant ssh -c "sudo cat $(FAASD_SECRETES_PATH)/basic-auth-password"

.PHONY: build-faasd
build-faasd:
	vagrant provision --provision-with build-faasd