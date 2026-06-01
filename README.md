# FaaS-Platform-Knowledge-Optimization

Optimize FaaS platform cold starts using platform knowledge

## Prerequisites

- [golang](https://go.dev/doc/install)
- [make](https://www.gnu.org/software/make/)
- [vagrant](https://www.vagrantup.com/docs/installation)

We suggest setting up your Go environment with [mise](https://github.com/jdx/mise).

## Environment Setup

If you run faasd tests in local-registry mode, a local registry must be running and accessible. You need to ensure that:

- Ensure `registry.local` resolves to the local registry IP (e.g., via `/etc/hosts` or DNS).
- Ensure Docker proxies and insecure registries are configured in your Docker daemon configuration (e.g., `$HOME/.docker/daemonjson` or `/etc/docker/daemon.json`) to allow pushing to `registry.local:5050` without TLS and bypassing proxies:

```json
"insecure-registries": [
  "registry.local:5050",
  "127.0.0.1:5050"
],
"proxies": {
  "no-proxy": "registry.local,localhost,127.0.0.0/8"
}
```

> Note: Proxy configurations specified in the daemon.json are ignored by Docker Desktop. If you use Docker Desktop, you can configure proxies using the Docker Desktop UI under Settings > Resources > Proxies.

## Submodules Initialization

This repository contains several git submodules for the faasd and tinyFaaS platforms. You need to initialize and update these submodules after cloning the repository:

```bash
git submodule update --init --recursive
```

## Integration Test Image Source (faasd)

The faasd integration helpers support two image source modes:

- `REGISTRY_TYPE=remote` (default): tests use pre-published images.
- `REGISTRY_TYPE=local`: tests build/push images to a local registry (for example `registry.local:5050`).

The stack files use a prefix template like `${REGISTRY_PREFIX:-registry.local:5050/faasd/}`. To override image prefixes for remote images, set `REGISTRY_PREFIX`.

You can use the following prefixes for public remote images for free:

```bash
export REGISTRY_PREFIX="ghcr.io/mactavishz/faasd-"
# or
export REGISTRY_PREFIX="macsalvation/faasd-"
```

By default, remote mode uses `macsalvation/faasd-`. Set `REGISTRY_PREFIX` if you want a different registry/namespace.

You can also publish your own multi-arch images and point tests to them:

```bash
REGISTRY_PREFIX="<prefix>" faas-cli publish --platforms linux/amd64,linux/arm64 -f ./<path-to-function>/stack.yaml
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

Faasd needs a local registry to be configured and running before it can start.

```bash
# Start the faasd VM
vagrant up faasd

# Start the local registry, build faasd, and start faasd
make build-faasd
```

The faasd gateway will be available at `http://127.0.0.1:8080` by default.

### Deploy faasd/tinyfaas to a Remote Host

You can deploy faasd/tinyfaas to a remote Linux host over SSH with the helper script below:

```bash
# for faasd
GITHUB_TOKEN=<github-pat> ./scripts/deploy-faasd.sh \
  --host user@server \
  --env-file .env

# for tinyfaas
GITHUB_TOKEN=<github-pat> ./scripts/deploy-tinyfaas.sh \
  --host user@server \
  --env-file .env
```

The script installs faasd/tinyfaas prerequisites, clones or updates this repository on the target host, uploads the provided env file, and runs the remote build/install flow.

### Start tinyFaaS

```bash
# Start the tinyFaaS VM
vagrant up tinyfaas

# Build and start tinyFaaS
make tinyfaas-up
```

The tinyFaaS gateway will be available at: `http://127.0.0.1:8080`

## Using the Local faas-cli

Once you've built `faas-cli` with `make build-faas-cli`, you can use it to deploy and invoke functions.

### Deploy a Function to OpenFaaS (faasd)

You can deploy functions using the provided stack file or specify handler and language directly.

```bash
# Deploy using the faasd stack file
faas-cli deploy -f faasd-stack.yml

# Deploy a single function specifying handler and language
faas-cli deploy --image=my_image --name=my_fn --handler=/path/to/fn/ \
  --gateway=http://remote-site.com:8080 --lang=python \
  --env=MYVAR=myval
```

### Deploy a Function to tinyFaaS

We have extended `faas-cli` to support tinyFaaS. You can deploy functions using the provided stack file or specify handler and language directly.

You should always use the `--platform tinyfaas` flag and specify the tinyFaaS gateway URL via the `--gateway` flag. when working with tinyFaaS.

```bash
# Deploy using the tinyFaaS stack file, you don't need the --gateway flag here if the gateway is specified in the stack file
faas-cli deploy -f tinyfaas-stack.yml --platform tinyfaas

# Deploy a single function specifying handler and language
faas-cli deploy --platform tinyfaas --gateway http://localhost:8080 \
  --name echo --handler ./tinyFaaS/test/fns/echo --lang python3
```

### Invoke a Function

```bash
# Invoke a function on OpenFaaS
echo "Hello World" | faas-cli invoke <function-name>

# Invoke a function on tinyFaaS
echo "Hello World" | faas-cli invoke hellopy --platform tinyfaas -g http://localhost:8080
```

### List Functions

```bash
# List functions on OpenFaaS
faas-cli list

# List functions on tinyFaaS
faas-cli list --gateway http://127.0.0.1:8080 --platform tinyfaas
```

### Remove a Function

```bash
# Remove from OpenFaaS
faas-cli remove <function-name>

# Remove from tinyFaaS
faas-cli remove hellopy --platform tinyfaas --gateway http://localhost:8080
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
  gateway: http://127.0.0.1:8080
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
