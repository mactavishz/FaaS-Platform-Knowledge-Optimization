# FaaS-Platform-Knowledge-Optimization

Optimize FaaS platform cold starts using platform knowledge

## Prerequisites

- [golang](https://go.dev/doc/install)
- [make](https://www.gnu.org/software/make/)
- [vagrant](https://www.vagrantup.com/docs/installation)

We suggest setting up your Go environment with [mise](https://github.com/jdx/mise).

## Environment Setup

Testing faasd requires a local registry to be running and accessible. You need to ensure that:

- Ensure `registry.local` resolves to the local registry IP (e.g., via `/etc/hosts` or DNS).
- Ensure Docker proxies and insecure registries are configured in your Docker daemon configuration (e.g., `$HOME/.docker/daemonjson` or `/etc/docker/daemon.json`) to allow pushing to `registry.local:5050` without TLS and bypassing proxies:

```json
"insecure-registries": [
  "registry.local:5050"
],
"proxies": {
  "default": {
    "noProxy": "registry.local,localhost,127.0.0.0/8"
  }
}
```

## Building the Project

### Start the VMs

```bash
# Start the Vagrant VMs
vagrant up faasd
vagrant up tinyfaas
```

### Build Individual Components

```bash
# Build only faas-cli
make build-faas-cli

# Build only faasd
make build-faasd

# Build only tinyFaaS
make build-tinyfaas
```

The built `faas-cli` binary will be placed in `$GOBIN` (typically `~/go/bin` or your configured Go bin directory).

## Running the Platform

### Start faasd (OpenFaaS)

Faasd needs a local registry to be configured and running before it can start. The `faasd-up` target will handle this for you.

```bash
# Start the faasd VM
vagrant up faasd

# Start the local registry, build faasd, and start faasd
make build-faasd
```

The OpenFaaS gateway will be available at `http://127.0.0.1:8080` by default.

### Start tinyFaaS

```bash
# Start the tinyFaaS VM
vagrant up tinyfaas

# Build and start tinyFaaS
make tinyfaas-up
```

The tinyFaaS gateway will be available at:

- API: `http://127.0.0.1:9090`
- Management: `http://127.0.0.1:9091`

## Using the Local faas-cli

Once you've built `faas-cli` with `make build-faas-cli`, you can use it to deploy and invoke functions.

### Deploy a Function to OpenFaaS (faasd)

TBD

### Deploy a Function to tinyFaaS

We have extended `faas-cli` to support tinyFaaS. You can deploy functions using the provided stack file or specify handler and language directly.

You should always use the `--platform tinyfaas` flag and specify the tinyFaaS gateway URL via the `--gateway` flag. when working with tinyFaaS.

```bash
# Deploy using the tinyFaaS stack file, you don't need the --gateway flag here if the gateway is specified in the stack file
faas-cli deploy -f tinyfaas-stack.yml --platform tinyfaas

# Deploy a single function specifying handler and language
faas-cli deploy --platform tinyfaas --gateway http://localhost:9091 \
  --name echo --handler ./tinyFaaS/test/fns/echo --lang python3
```

### Invoke a Function

```bash
# Invoke a function on OpenFaaS
echo "Hello World" | faas-cli invoke <function-name>

# Invoke a function on tinyFaaS
echo "Hello World" | faas-cli invoke hellopy --platform tinyfaas -g http://localhost:9090
```

### List Functions

```bash
# List functions on OpenFaaS
faas-cli list

# List functions on tinyFaaS
faas-cli list --gateway http://127.0.0.1:9091 --platform tinyfaas
```

### Remove a Function

```bash
# Remove from OpenFaaS
faas-cli remove <function-name>

# Remove from tinyFaaS
faas-cli remove hellopy --platform tinyfaas --gateway http://localhost:9091
```

## Example Stack Files

### For faasd (OpenFaaS)

You can use the stack file as described in the [OpenFaaS documentation](https://docs.openfaas.com/reference/yaml/)

Here is an example:

```yaml
# faasd-stack.yml
provider:
  name: openfaas
  gateway: http://127.0.0.1:8080
functions:
  env:
    image: ghcr.io/openfaas/alpine:latest
    fprocess: env
  nodeinfo:
    image: ghcr.io/openfaas/nodeinfo:latest
```

Then deploy with:

```bash
faas-cli deploy -f faasd-stack.yml
```

### For tinyFaaS

For tinyFaaS, you don't have to specify the image. Instead, you provide the handler path and language.

```yaml
# tinyfaas-stack.yml
provider:
  name: tinyfaas
  gateway: http://127.0.0.1:9091
functions:
  hellopy:
    lang: python3
    handler: ./tinyFaaS/test/fns/echo
  hellonode:
    lang: nodejs
    handler: ./tinyFaaS/test/fns/echo-js
```

Then deploy with:

```bash
faas-cli deploy -f tinyfaas-stack.yml
```

## Troubleshooting

- If `faas-cli` is not in your PATH, make sure `$GOBIN` is added to your PATH or use the full path to the binary
- For faasd authentication issues, run `make faasd-login` again
- To restart the VMs: `vagrant halt <vm-name> && vagrant up <vm-name>`
