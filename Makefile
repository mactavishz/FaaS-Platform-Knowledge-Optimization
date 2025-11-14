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

.PHONY: build-faas-cli
build-faas-cli:
	# run make command inside faas-cli directory
	make -C faas-cli local-install

.PHONY: clean-faas-cli
clean-faas-cli:
	make -C faas-cli clean

.PHONY: clean-faasd
clean-faasd:
	make -C faasd clean
	
.PHONY: clean-tinyFaaS
clean-tinyFaaS:
	make -C tinyFaaS clean
	
.PHONY: clean
clean: clean-faasd clean-faas-cli clean-tinyFaaS