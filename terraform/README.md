# Terraform

This directory provisions a Google Cloud VM for benchmarking either tinyFaaS or faasd.

The VM startup script clones this repository, initializes submodules, writes the selected platform environment file, builds the platform from source, starts its systemd services, and waits for the gateway to become reachable.

## Prerequisites

- [Terraform](https://developer.hashicorp.com/terraform/install)
- Google Cloud credentials set in `GOOGLE_CREDENTIALS`
- Google Project ID set in `GOOGLE_PROJECT`
- An SSH key pair for VM access
- A GitHub token that can clone this repository and its submodules
- A platform `.env` file if you want deployment-specific overrides

Secrets are passed through Terraform variables and rendered into GCE startup metadata. Terraform marks the token, env file path, and faasd password output as sensitive where possible, but Terraform state and instance metadata should still be treated as sensitive.

### Generating a pair of SSH keys

You can generate a new SSH key pair with the following command. When prompted for a file to save the key, you can press Enter to accept the default path (`~/.ssh/id_ed25519`), or specify a custom path if you prefer:

```bash
# rsa is also supported but ed25519 is recommended for better security and performance
ssh-keygen -t ed25519 -C "gcp-vm"
```

## Usage

Run the following commands from the repository root:

```bash
export TF_VAR_github_token="$GITHUB_TOKEN"

# You can also cd into the terraform directory and run terraform commands without the -chdir flag
terraform -chdir=terraform init
# Assuming you have the default tinyFaaS env file at .env and ssh public key at ~/.ssh/id_ed25519.pub:
terraform -chdir=terraform apply \
  -var 'faas_platforms=["tinyfaas"]' \
  -var env_file="$PWD/.env" \
  -var ssh_pubkey="$HOME/.ssh/id_ed25519.pub"
```

Use `-var 'faas_platforms=["faasd"]'` to provision faasd instead, or `-var 'faas_platforms=["faasd","tinyfaas"]'` to provision both platforms in one Terraform state.

The default gateway port is `8080`. tinyFaaS consumes `GATEWAY_PORT` from `/etc/default/tinyfaas`. The current faasd gateway binary listens on `8080`, so use the default for faasd unless the gateway implementation is updated to support a configurable listen port.

The startup script builds from a `bench`-owned checkout at `/opt/faas-platform` by default and uses Go caches under `/home/bench`. Root is still used for package installation, systemd units, and files under `/etc/default`.

## Outputs

```bash
terraform -chdir=terraform output instance_names
terraform -chdir=terraform output zones
terraform -chdir=terraform output public_ips
terraform -chdir=terraform output gateway_urls
terraform -chdir=terraform output ssh_commands
```

## Access

SSH:

```bash
ssh -i "<private-key-path>" bench@$(terraform -chdir=terraform output -json public_ips | jq -r .tinyfaas)
```

Or use the placeholder output and replace `<private-key-path>`:

```bash
terraform -chdir=terraform output -raw ssh_command
```

For faasd, Terraform manages the basic-auth credentials:

```bash
terraform -chdir=terraform output faasd_auth_users
terraform -chdir=terraform output faasd_auth_passwords
```

Or, you can combine the outputs to `faas-cli` commands:

```bash
# Login to faasd with the Terraform outputs
terraform output -json faasd_auth_passwords | jq -r .faasd | faas-cli login -s -g "$(terraform output -json gateway_urls | jq -r .faasd)"

# Then verify the function list is accessible
faas-cli list
```

## Debugging

If Terraform is still waiting on the gateway health check, inspect the serial console without SSH:

```bash
gcloud compute instances get-serial-port-output \
  "$(terraform -chdir=terraform output -json instance_names | jq -r .tinyfaas)" \
  --zone "$(terraform -chdir=terraform output -json zones | jq -r .tinyfaas)" \
  --port 1
```

On the VM:

```bash
sudo tail -f /var/log/vm-provision.log
sudo journalctl -u google-startup-scripts.service -f
```

tinyFaaS services:

```bash
sudo journalctl -u tf-gateway -o cat -f
sudo journalctl -u tf-rproxy -o cat -f
sudo journalctl -u tf-manager -o cat -f
```

faasd services:

```bash
sudo journalctl -u faasd -o cat -f
sudo journalctl -u faasd-provider -o cat -f
sudo journalctl -u faasd-gateway -o cat -f
```

## Local Terraform Checks

```bash
terraform -chdir=terraform fmt -check -diff
terraform -chdir=terraform validate
bash -n terraform/scripts/provision_faasd_tpl.sh terraform/scripts/provision_tinyfaas_tpl.sh
shellcheck terraform/scripts/provision_faasd_tpl.sh terraform/scripts/provision_tinyfaas_tpl.sh
```
